package agent

import (
	"context"
	"log"
	"strings"
	"sync"

	"voiceagent/internal/think"
)

func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	p.session.SetState(StateProcessing)

	// Grab accumulated thinker output before clearing
	previousThought := p.getDraftOutput()
	p.resetDraftOutput()

	p.history.AddUser(userText)

	contextMsgs := p.drainContextQueue()
	contextPrompt := FormatContextForLLM(contextMsgs)

	inRequirementsMode := false
	systemPrompt := p.config.SystemPrompt
	p.session.reqMu.RLock()
	reqSnapshot := CloneTaskRequirements(p.session.Requirements)
	p.session.reqMu.RUnlock()
	if reqSnapshot != nil && (reqSnapshot.Status == "collecting" || reqSnapshot.Status == "confirming") {
		inRequirementsMode = true
		var profile *UserProfile
		if p.clients != nil {
			if pInfo, err := p.clients.GetUserProfile(ctx, reqSnapshot.UserID); err == nil {
				profile = &pInfo
			}
		}
		systemPrompt = reqSnapshot.BuildRequirementsSystemPrompt(profile)
	}

	if !inRequirementsMode {
		systemPrompt += pptIntentDetectionPrompt
	}

	taskListContext := p.buildTaskListContext()
	if taskListContext != "" {
		systemPrompt += taskListContext
	}
	pendingQContext := p.buildPendingQuestionsContext()
	if pendingQContext != "" {
		systemPrompt += pendingQContext
	}
	if contextPrompt != "" {
		systemPrompt += contextPrompt
	}

	log.Printf("Processing user input: %s", truncate(userText, 100))
	if previousThought != "" {
		log.Printf("With %d chars of pre-thinking", len(previousThought))
	}

	// Send user text to browser for display
	p.session.SendJSON(WSMessage{Type: "transcript", Text: userText})

	// TTS sentence queue — decouples LLM generation from TTS synthesis
	sentenceCh := make(chan string, p.adaptive.Get("sentence_ch"))

	var ttsWg sync.WaitGroup
	ttsWg.Add(1)
	go func() {
		defer ttsWg.Done()
		p.ttsWorker(ctx, sentenceCh)
	}()

	// Stream tokens from Large LLM (with accumulated thought if available).
	messages := p.history.ToOpenAIWithThoughtAndPrompt(previousThought, systemPrompt)
	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	totalTokens := 0 // ALL tokens (including <think>) for budget accounting
	var sentenceBuf strings.Builder
	var allTokens strings.Builder
	firstSentenceSent := false
	nextFillerAt := p.config.TokenBudget // first filler fires at TokenBudget
	fillerCount := 0

	var tf think.ThinkFilter

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

		// Track ALL raw tokens (including <think>) for interrupt preservation
		p.tokensMu.Lock()
		p.rawGeneratedTokens.WriteString(token)
		p.tokensMu.Unlock()

		// Strip <think>...</think> blocks — only pass visible content through
		visible := tf.Feed(token)

		if visible != "" {
			allTokens.WriteString(visible)
			p.session.SendJSON(WSMessage{Type: "response", Text: visible})
		}

		// Budget window counts ALL tokens: gives the model time to think
		// internally, but if the budget runs out with nothing spoken, inject filler.
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

		// Periodic filler while model is still thinking and no visible sentence produced.
		// Each filler is a different phrase; stop after MaxFillers to avoid sounding robotic.
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

	if flushed := tf.Flush(); flushed != "" {
		allTokens.WriteString(flushed)
		sentenceBuf.WriteString(flushed)
	}
	if sentenceBuf.Len() > 0 {
		sendSentence(sentenceBuf.String())
	}
	close(sentenceCh)

	// Wait for all TTS audio to be sent
	ttsWg.Wait()

	// Save to history (if not interrupted)
	if ctx.Err() == nil {
		finalText := allTokens.String()
		if finalText != "" {
			p.history.AddAssistant(finalText)
		}

		p.postProcessResponse(ctx, userText, finalText, inRequirementsMode)
		p.asyncExtractMemory(userText, finalText)

		p.session.SetState(StateIdle)
	}

	p.adaptive.Adjust()
	p.adaptive.Save(p.config.AdaptiveSizesFile)
}

func (p *Pipeline) postProcessResponse(ctx context.Context, userText, llmResponse string, inRequirementsMode bool) {
	if p.tryResolveConflict(ctx, userText, llmResponse) {
		return
	}

	if inRequirementsMode {
		p.handleRequirementsTransition(llmResponse)
		return
	}

	if p.tryDetectTaskInit(llmResponse) {
		return
	}

	p.trySendPPTFeedback(userText, llmResponse)
}
