package agent

import (
	"context"
	"log"
	"strings"
	"time"

	"voiceagent/internal/asr"
)

// StartListening runs the listening pipeline: accumulate audio, run ASR,
// check for interrupt intent, and optionally do draft thinking.
func (p *Pipeline) StartListening(ctx context.Context) {
	p.runMu.Lock()
	defer p.runMu.Unlock()

	audioCh := make(chan []byte, p.adaptive.Get("audio_ch"))
	vadEndCh := make(chan struct{}, 1)
	p.ioMu.Lock()
	p.audioBuf.Reset()
	p.audioCh = audioCh
	p.vadEndCh = vadEndCh
	p.ioMu.Unlock()
	defer func() {
		p.ioMu.Lock()
		if p.audioCh == audioCh {
			p.audioCh = nil
		}
		if p.vadEndCh == vadEndCh {
			p.vadEndCh = nil
		}
		p.ioMu.Unlock()
	}()

	go p.highPriorityListener(ctx)

	p.tokensMu.Lock()
	p.rawGeneratedTokens.Reset()
	p.tokensMu.Unlock()

	p.resetDraftOutput()

	asrAudioCh := make(chan []byte, p.adaptive.Get("asr_audio_ch"))

	asrResultCh, err := p.asrClient.RecognizeStream(ctx, asrAudioCh, p.adaptive.Get("asr_result_ch"))
	if err != nil {
		log.Printf("ASR start failed: %v", err)
		p.session.SetState(StateIdle)
		return
	}

	var partialTexts []string
	var finalText string
	vadEnded := false
	draftStarted := false
	asrSinceDraft := 0
	draftInterval := 1
	contextQueriesStarted := false

	for {
		select {
		case audio := <-audioCh:
			p.audioBuf.Write(audio)

			// Extract complete 1s blocks and send to ASR
			for {
				block, ok := p.audioBuf.GetBlock()
				if !ok {
					break
				}
				p.adaptive.RecordLen("asr_audio_ch", len(asrAudioCh))
				select {
				case asrAudioCh <- block:
				case <-ctx.Done():
					close(asrAudioCh)
					return
				}
			}

		case result, ok := <-asrResultCh:
			if !ok {
				fullText := finalText
				if fullText == "" {
					fullText = strings.Join(partialTexts, "")
				}
				if fullText != "" {
					p.startProcessing(ctx, fullText)
				} else {
					p.session.SetState(StateIdle)
				}
				return
			}

			if result.Mode == "2pass-offline" {
				finalText = result.Text
				p.session.SendJSON(WSMessage{Type: "transcript_final", Text: finalText})
			} else {
				partialTexts = append(partialTexts, result.Text)
				partialText := strings.Join(partialTexts, "")
				p.session.SendJSON(WSMessage{Type: "transcript", Text: partialText})
			}

			// Draft thinking uses partial text (low latency)
			currentText := strings.Join(partialTexts, "")
			if currentText == "" && finalText != "" {
				currentText = finalText
			}
			if !contextQueriesStarted && currentText != "" {
				contextQueriesStarted = true
				p.launchAsyncContextQueries(ctx, currentText)
			}
			if currentText == "" {
				break
			}

			if !draftStarted {
				if isInterrupt(ctx, p.smallLLM, currentText) {
					draftStarted = true
					asrSinceDraft = 0
					draftInterval = 2
					p.startDraftThinking(ctx, currentText)
				}
			} else {
				asrSinceDraft++
				if asrSinceDraft >= draftInterval {
					asrSinceDraft = 0
					draftInterval++
					p.startDraftThinking(ctx, currentText)
				}
			}

		case <-vadEndCh:
			vadEnded = true
			if remaining := p.audioBuf.Flush(); len(remaining) > 0 {
				select {
				case asrAudioCh <- remaining:
				default:
				}
			}
			close(asrAudioCh)

			// Wait for final ASR results (including 2pass-offline)
			p.drainASRResults(ctx, asrResultCh, &partialTexts, &finalText, 2*time.Second)

			p.cancelDraft()

			fullText := finalText
			if fullText == "" {
				fullText = strings.Join(partialTexts, "")
			}
			if fullText == "" {
				p.session.SetState(StateIdle)
				return
			}
			if finalText != "" {
				p.session.SendJSON(WSMessage{Type: "transcript_final", Text: finalText})
			}
			p.startProcessing(ctx, fullText)
			return

		case <-ctx.Done():
			if !vadEnded {
				close(asrAudioCh)
			}
			return
		}
	}
}

// drainASRResults reads remaining ASR results until timeout or channel close.
// 2pass-offline results update finalText; others append to partialTexts.
func (p *Pipeline) drainASRResults(ctx context.Context, ch <-chan asr.ASRResult, partialTexts *[]string, finalText *string, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case result, ok := <-ch:
			if !ok {
				return
			}
			if result.Text == "" {
				continue
			}
			if result.Mode == "2pass-offline" {
				*finalText = result.Text
			} else {
				*partialTexts = append(*partialTexts, result.Text)
			}
			if result.IsFinal {
				return
			}
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// ---------------------------------------------------------------------------
// 边听边想: Draft thinking
// ---------------------------------------------------------------------------

func (p *Pipeline) startDraftThinking(ctx context.Context, partialText string) {
	previousThought := p.getDraftOutput()
	p.cancelDraft()
	p.resetDraftOutput()

	p.draftMu.Lock()
	draftCtx, cancel := context.WithCancel(ctx)
	p.draftCancel = cancel
	p.draftMu.Unlock()

	go func() {
		messages := p.history.ToOpenAIWithDraftAndThought(partialText, previousThought)
		tokenCh := p.largeLLM.StreamChat(draftCtx, messages)
		for token := range tokenCh {
			p.appendDraftOutput(token)
		}
	}()
}

func (p *Pipeline) cancelDraft() {
	p.draftMu.Lock()
	if p.draftCancel != nil {
		p.draftCancel()
		p.draftCancel = nil
	}
	p.draftMu.Unlock()
}

func (p *Pipeline) getDraftOutput() string {
	p.draftOutputMu.Lock()
	defer p.draftOutputMu.Unlock()
	return p.draftOutput.String()
}

func (p *Pipeline) appendDraftOutput(token string) {
	p.draftOutputMu.Lock()
	p.draftOutput.WriteString(token)
	p.draftOutputMu.Unlock()
}

func (p *Pipeline) resetDraftOutput() {
	p.draftOutputMu.Lock()
	p.draftOutput.Reset()
	p.draftOutputMu.Unlock()
}
