package agent

import (
	"context"
	"log"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// O/T/A Concurrent Architecture
// StartInteractive runs the concurrent O/T/A pipeline.
func (p *Pipeline) StartInteractive(ctx context.Context) {
	p.runMu.Lock()
	defer p.runMu.Unlock()

	p.sessionCtxMu.Lock()
	p.sessionCtx = ctx
	p.sessionCtxMu.Unlock()

	// Initialize channels
	p.ioMu.Lock()
	audioCh := make(chan []byte, p.adaptive.Get("audio_ch"))
	vadEndCh := make(chan struct{}, 1)
	p.audioCh = audioCh
	p.vadEndCh = vadEndCh
	p.userInputCh = make(chan string, 10)
	p.tokenCh = make(chan string, 100)
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

	// Reset token tracking
	p.tokensMu.Lock()
	p.rawGeneratedTokens.Reset()
	p.tokensMu.Unlock()

	// Start high-priority listener
	go p.highPriorityListener(ctx)

	// Start concurrent loops
	var wg sync.WaitGroup
	wg.Add(4)

	go func() {
		defer wg.Done()
		p.asrLoop(ctx)
	}()

	go func() {
		defer wg.Done()
		p.thinkLoop(ctx)
	}()

	go func() {
		defer wg.Done()
		p.outputLoop(ctx)
	}()

	go func() {
		defer wg.Done()
		p.ttsWorker(ctx, p.sentenceCh)
	}()

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
		log.Printf("ASR start failed: %v", err)
		p.session.SetState(StateIdle)
		return
	}

	var partialTexts []string

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
				return
			}

			if result.Mode == "2pass-offline" {
				// 2pass 是基于全部音频的最终结果，替换之前的流式累积
				partialTexts = nil
				select {
				case p.userInputCh <- result.Text:
				case <-ctx.Done():
					return
				}
				p.session.SendJSON(WSMessage{Type: "transcript_final", Text: result.Text})
			} else {
				// 流式结果：累积并发送
				partialTexts = append(partialTexts, result.Text)
				currentText := strings.Join(partialTexts, "")
				select {
				case p.userInputCh <- currentText:
				default:
				}
				p.session.SendJSON(WSMessage{Type: "transcript", Text: currentText})
			}

		case <-vadEndCh:
			close(asrAudioCh)
			return

		case <-ctx.Done():
			return
		}
	}
}
