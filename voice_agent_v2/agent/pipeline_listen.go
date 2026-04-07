package agent

import (
	"context"
	"log"
	"strings"
	"sync"
)

// StartInteractive runs the voice pipeline for a session.
// ASR → thinkLoop (KV warmup) + ttsWorker run concurrently.
func (p *Pipeline) StartInteractive(ctx context.Context) {
	p.runMu.Lock()
	defer p.runMu.Unlock()

	p.sessionCtxMu.Lock()
	p.sessionCtx = ctx
	p.sessionCtxMu.Unlock()

	p.ioMu.Lock()
	audioCh := make(chan []byte, p.adaptive.Get("audio_ch"))
	vadEndCh := make(chan struct{}, 1)
	p.audioCh = audioCh
	p.vadEndCh = vadEndCh
	p.userInputCh = make(chan string, 10)
	p.sentenceCh = make(chan string, p.adaptive.Get("sentence_ch"))
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

	p.tokensMu.Lock()
	p.rawTokens.Reset()
	p.tokensMu.Unlock()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); p.asrLoop(ctx) }()
	go func() { defer wg.Done(); p.thinkLoop(ctx) }()
	go func() { defer wg.Done(); p.ttsWorker(ctx, p.sentenceCh) }()
	wg.Wait()
}

func (p *Pipeline) asrLoop(ctx context.Context) {
	p.ioMu.RLock()
	audioCh := p.audioCh
	vadEndCh := p.vadEndCh
	p.ioMu.RUnlock()

	asrAudioCh := make(chan []byte, p.adaptive.Get("asr_audio_ch"))
	asrResultCh, err := p.asrClient.RecognizeStream(ctx, asrAudioCh, p.adaptive.Get("asr_result_ch"))
	if err != nil {
		log.Printf("[asr] start failed: %v", err)
		p.session.SetState(StateIdle)
		return
	}

	var partialTexts []string
	var latestPartial, finalText string
	var gotFinal, gotVADEnd, audioClosed, finalLaunched bool

	launchFinal := func() {
		if finalLaunched || !gotVADEnd {
			return
		}
		text := strings.TrimSpace(finalText)
		if text == "" {
			return
		}
		finalLaunched = true
		procCtx := p.session.newPipelineContext()
		go p.startProcessing(procCtx, text)
	}

	for {
		select {
		case audio := <-audioCh:
			p.audioBuf.Write(audio)
			for {
				block, ok := p.audioBuf.GetBlock()
				if !ok {
					break
				}
				select {
				case asrAudioCh <- block:
				case <-ctx.Done():
					return
				}
			}

		case result, ok := <-asrResultCh:
			if !ok {
				if gotVADEnd && !gotFinal {
					finalText = latestPartial
				}
				launchFinal()
				return
			}
			if result.Mode == "2pass-offline" || result.IsFinal {
				partialTexts = nil
				finalText = result.Text
				gotFinal = true
				p.session.SendJSON(WSMessage{Type: "transcript_final", Text: result.Text})
				launchFinal()
			} else {
				partialTexts = append(partialTexts, result.Text)
				current := strings.Join(partialTexts, "")
				latestPartial = current
				select {
				case p.userInputCh <- current:
				default:
				}
				p.session.SendJSON(WSMessage{Type: "transcript", Text: current})
			}

		case <-vadEndCh:
			gotVADEnd = true
			if !audioClosed {
				close(asrAudioCh)
				audioClosed = true
			}
			launchFinal()

		case <-ctx.Done():
			return
		}
	}
}
