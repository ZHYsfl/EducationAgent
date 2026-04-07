package agent

import (
	"context"
	"log"
	"strings"
	"sync"

	"voiceagent/internal/protocol"
)

func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	p.session.SetState(StateProcessing)

	p.history.AddUser(userText)

	systemPrompt := p.buildFullSystemPrompt(ctx, true)

	log.Printf("Processing user input: %s", truncate(userText, 100))

	// Send user text to browser for display before LLM starts
	p.session.SendJSON(WSMessage{Type: "transcript", Text: userText})

	// TTS sentence queue decouples LLM generation from TTS synthesis
	sentenceCh := make(chan string, p.adaptive.Get("sentence_ch"))

	var ttsWg sync.WaitGroup
	ttsWg.Add(1)
	go func() {
		defer ttsWg.Done()
		p.ttsWorker(ctx, sentenceCh)
	}()

	// Stream tokens from Large LLM
	previousThought := p.consumeThinkDraft()
	messages := p.history.ToOpenAIWithThoughtAndPrompt(previousThought, systemPrompt)
	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	totalTokens := 0
	var sentenceBuf strings.Builder
	var allTokens strings.Builder
	var allActions []protocol.Action
	firstSentenceSent := false
	nextFillerAt := p.config.TokenBudget
	fillerCount := 0

	var pf protocol.ProtocolFilter

	// Interrupt-safe send: never blocks if ttsWorker already exited.
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

		// Track ALL raw tokens for interrupt preservation
		p.tokensMu.Lock()
		p.rawGeneratedTokens.WriteString(token)
		p.tokensMu.Unlock()

		result := p.parser.Feed(token)
		allActions = append(allActions, result.Actions...)

		for _, action := range result.Actions {
			p.executeAction(ctx, action)
		}

		visible := pf.Feed(token)
		if visible != "" {
			allTokens.WriteString(visible)
			p.session.SendJSON(WSMessage{Type: "response", Text: visible})
		}

		// Within token budget: accumulate and send sentences normally.
		if totalTokens <= p.config.TokenBudget {
			if visible != "" {
				sentenceBuf.WriteString(visible)
				if isSentenceEnd(visible) && sentenceBuf.Len() > 0 {
					sentence := sentenceBuf.String()
					sentenceBuf.Reset()
					if !sendSentence(sentence) {
						break
					}
					firstSentenceSent = true
					if p.session.GetState() == StateProcessing {
						p.session.SetState(StateSpeaking)
					}
				}
			}
			continue
		}

		// Beyond budget: inject filler if model hasn't spoken yet.
		if !firstSentenceSent && fillerCount < p.config.MaxFillers && totalTokens >= nextFillerAt {
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
			if isSentenceEnd(visible) {
				sentence := sentenceBuf.String()
				sentenceBuf.Reset()
				if !sendSentence(sentence) {
					break
				}
				firstSentenceSent = true
			}
		}
	}

	if sentenceBuf.Len() > 0 {
		sendSentence(sentenceBuf.String())
	}
	close(sentenceCh)
	ttsWg.Wait()

	if ctx.Err() == nil {
		finalText := allTokens.String()
		if finalText != "" {
			p.history.AddAssistant(finalText)
		}
		p.postProcessResponse(ctx, userText, finalText, allActions)
		p.maybeCompressHistory()
	}
	p.session.SetState(StateIdle)

	p.adaptive.Adjust()
	p.adaptive.Save(p.config.AdaptiveSizesFile)
}
