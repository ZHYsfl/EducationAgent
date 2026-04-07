package agent

import (
	"context"
	"log"
	"strings"
	"sync"

	"voiceagentv2/internal/protocol"
)

// startProcessing is the main LLM processing round.
// Called after VAD end (final ASR text) or direct text input.
func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	p.session.SetState(StateProcessing)
	p.history.AddUser(userText)
	p.session.SendJSON(WSMessage{Type: "transcript", Text: userText})

	systemPrompt := p.buildSystemPrompt(true)
	previousThought := p.consumeThinkDraft()
	messages := p.history.ToOpenAIWithThoughtAndPrompt(previousThought, systemPrompt)

	log.Printf("[pipeline] processing: %s", truncate(userText, 80))

	sentenceCh := make(chan string, p.adaptive.Get("sentence_ch"))
	var ttsWg sync.WaitGroup
	ttsWg.Add(1)
	go func() {
		defer ttsWg.Done()
		p.ttsWorker(ctx, sentenceCh)
	}()

	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	var (
		allVisible   strings.Builder
		sentenceBuf  strings.Builder
		pf           protocol.ProtocolFilter
		totalTokens  int
		fillerCount  int
		nextFillerAt = p.config.TokenBudget
		firstSent    bool
	)

	sendSentence := func(s string) bool {
		p.adaptive.RecordLen("sentence_ch", len(sentenceCh))
		select {
		case sentenceCh <- s:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for token := range tokenCh {
		if ctx.Err() != nil {
			break
		}
		totalTokens++

		p.tokensMu.Lock()
		p.rawTokens.WriteString(token)
		p.tokensMu.Unlock()

		result := p.parser.Feed(token)
		for _, action := range result.Actions {
			p.executeAction(ctx, action)
		}

		visible := pf.Feed(token)
		if visible != "" {
			allVisible.WriteString(visible)
			p.session.SendJSON(WSMessage{Type: "response", Text: visible})
		}

		if totalTokens <= p.config.TokenBudget {
			if visible != "" {
				sentenceBuf.WriteString(visible)
				if isSentenceEnd(visible) && sentenceBuf.Len() > 0 {
					if !sendSentence(sentenceBuf.String()) {
						break
					}
					sentenceBuf.Reset()
					firstSent = true
					if p.session.GetState() == StateProcessing {
						p.session.SetState(StateSpeaking)
					}
				}
			}
			continue
		}

		// Beyond token budget: inject filler if model hasn't spoken yet
		if !firstSent && fillerCount < p.config.MaxFillers && totalTokens >= nextFillerAt {
			idx := fillerCount
			if idx >= len(p.config.FillerPhrases) {
				idx = len(p.config.FillerPhrases) - 1
			}
			if !sendSentence(p.config.FillerPhrases[idx]) {
				break
			}
			fillerCount++
			nextFillerAt = totalTokens + p.config.FillerInterval
			if p.session.GetState() == StateProcessing {
				p.session.SetState(StateSpeaking)
			}
		}

		if visible != "" {
			sentenceBuf.WriteString(visible)
			if isSentenceEnd(visible) && sentenceBuf.Len() > 0 {
				if !sendSentence(sentenceBuf.String()) {
					break
				}
				sentenceBuf.Reset()
				firstSent = true
			}
		}
	}

	if sentenceBuf.Len() > 0 {
		sendSentence(sentenceBuf.String())
	}
	close(sentenceCh)
	ttsWg.Wait()

	if ctx.Err() == nil {
		finalText := allVisible.String()
		if finalText != "" {
			p.history.AddAssistant(finalText)
		}
		p.maybeCompressHistory()
	}

	p.session.SetState(StateIdle)
	p.adaptive.Adjust()
	p.adaptive.Save(p.config.AdaptiveSizesFile)
}
