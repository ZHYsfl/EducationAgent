package agent

import (
	"context"
	"log"
	"strings"
	"sync"

	"voiceagent/internal/executor"
	"voiceagent/internal/protocol"
)

func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	p.session.SetState(StateProcessing)

	p.history.AddUser(userText)

	systemPrompt := p.buildFullSystemPrompt(ctx, true)

	log.Printf("Processing user input: %s", truncate(userText, 100))

	// Send user text to browser for display before LLM starts
	p.session.SendJSON(WSMessage{Type: "transcript", Text: userText})

	// TTS sentence queue — decouples LLM generation from TTS synthesis
	sentenceCh := make(chan string, p.adaptive.Get("sentence_ch"))

	var ttsWg sync.WaitGroup
	ttsWg.Add(1)
	go func() {
		defer ttsWg.Done()
		p.ttsWorker(ctx, sentenceCh)
	}()

	// Stream tokens from Large LLM
	messages := p.history.ToOpenAIWithThoughtAndPrompt("", systemPrompt)
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

		// Parse actions from the accumulated buffer
		result := p.parser.Feed(token)

		// Accumulate actions
		allActions = append(allActions, result.Actions...)

		// Execute any detected actions asynchronously
		for _, action := range result.Actions {
			reqs := p.session.GetRequirements()
			if reqs == nil {
				reqs = &TaskRequirements{}
			}
			sessionCtx := executor.SessionContext{
				UserID:            p.session.UserID,
				SessionID:         p.session.SessionID,
				ActiveTaskID:      p.session.ActiveTaskID,
				ViewingPageID:     p.session.ViewingPageID,
				BaseTimestamp:     p.session.LastVADTimestamp,
				Topic:             reqs.Topic,
				Subject:           reqs.Subject,
				TotalPages:        reqs.TotalPages,
				Audience:          reqs.TargetAudience,
				GlobalStyle:       reqs.GlobalStyle,
				KnowledgePoints:   reqs.KnowledgePoints,
				TeachingGoals:     reqs.TeachingGoals,
				TeachingLogic:     reqs.TeachingLogic,
				KeyDifficulties:   reqs.KeyDifficulties,
				Duration:          reqs.Duration,
				InteractionDesign: reqs.InteractionDesign,
				OutputFormats:     reqs.OutputFormats,
				ReferenceFiles:    reqs.ReferenceFiles,
			}
			p.executor.Execute(action, sessionCtx, p.EnqueueContext)
		}

		// Filter out #{...} and @{...} for display/TTS
		visible := pf.Feed(token)

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

	// Send any remaining buffered sentence
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

		p.postProcessResponse(ctx, userText, finalText, allActions)
		p.asyncPushContext(userText, finalText)

		p.session.SetState(StateIdle)
	} else {
		p.session.SetState(StateIdle)
	}

	p.adaptive.Adjust()
	p.adaptive.Save(p.config.AdaptiveSizesFile)
}

func (p *Pipeline) postProcessResponse(ctx context.Context, userText, _ string, actions []protocol.Action) {
	if p.tryResolveConflict(ctx, userText, actions) {
		return
	}

	// Tool calling 已在流式输出过程中自动处理
}

// ---------------------------------------------------------------------------
// O/T/A Concurrent Loops
// ---------------------------------------------------------------------------

func (p *Pipeline) thinkLoop(ctx context.Context) {
	var lastThinkLen int
	var isWarmup bool = true

	for {
		select {
		case userInput := <-p.userInputCh:
			inputLen := len([]rune(userInput))

			// 前7字：预热 KV-cache（抛弃输出）
			if inputLen < 7 {
				p.cancelThinkStream()
				streamCtx, cancel := context.WithCancel(ctx)
				p.setThinkCancel(cancel)
				isWarmup = true
				go p.runThinkStream(streamCtx, userInput, true)
				lastThinkLen = inputLen
				continue
			}

			// >= 7字：开始保留输出
			if isWarmup {
				p.cancelThinkStream()
				streamCtx, cancel := context.WithCancel(ctx)
				p.setThinkCancel(cancel)
				isWarmup = false
				go p.runThinkStream(streamCtx, userInput, false)
				lastThinkLen = inputLen
				continue
			}

			// 递增间隔 (7, 14, 21, 28...)
			threshold := ((lastThinkLen / 7) + 1) * 7
			if inputLen >= threshold {
				p.cancelThinkStream()
				streamCtx, cancel := context.WithCancel(ctx)
				p.setThinkCancel(cancel)
				go p.runThinkStream(streamCtx, userInput, false)
				lastThinkLen = inputLen
			}

		case <-ctx.Done():
			p.cancelThinkStream()
			return
		}
	}
}

func (p *Pipeline) setThinkCancel(cancel context.CancelFunc) {
	p.thinkCancelMu.Lock()
	p.thinkCancel = cancel
	p.thinkCancelMu.Unlock()
}

func (p *Pipeline) cancelThinkStream() {
	p.thinkCancelMu.Lock()
	if p.thinkCancel != nil {
		p.thinkCancel()
		p.thinkCancel = nil
	}
	p.thinkCancelMu.Unlock()
}

func (p *Pipeline) runThinkStream(ctx context.Context, userInput string, warmup bool) {
	p.history.AddUser(userInput)

	systemPrompt := p.buildSystemPrompt(ctx)

	messages := p.history.ToOpenAIWithThoughtAndPrompt("", systemPrompt)
	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	for token := range tokenCh {
		if warmup {
			// 预热阶段：抛弃输出，只为了预热 KV-cache
			continue
		}
		select {
		case p.tokenCh <- token:
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pipeline) buildSystemPrompt(ctx context.Context) string {
	return p.buildFullSystemPrompt(ctx, false)
}

func (p *Pipeline) outputLoop(ctx context.Context) {
	var sentenceBuf strings.Builder

	for {
		select {
		case token := <-p.tokenCh:
			result := p.parser.Feed(token)

			// 执行动作
			for _, action := range result.Actions {
				go p.executeAction(ctx, action)
			}

			// 累积可见文本
			if result.VisibleText != "" {
				sentenceBuf.WriteString(result.VisibleText)
				p.session.SendJSON(WSMessage{Type: "response", Text: result.VisibleText})

				if isSentenceEnd(result.VisibleText) {
					sentence := sentenceBuf.String()
					sentenceBuf.Reset()

					select {
					case p.sentenceCh <- sentence:
					case <-ctx.Done():
						return
					}

					if p.session.GetState() != StateSpeaking {
						p.session.SetState(StateSpeaking)
					}
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

func (p *Pipeline) executeAction(_ context.Context, action protocol.Action) {
	reqs := p.session.GetRequirements()
	if reqs == nil {
		reqs = &TaskRequirements{}
	}
	sessionCtx := executor.SessionContext{
		UserID:            p.session.UserID,
		SessionID:         p.session.SessionID,
		ActiveTaskID:      p.session.ActiveTaskID,
		ViewingPageID:     p.session.ViewingPageID,
		BaseTimestamp:     p.session.LastVADTimestamp,
		Topic:             reqs.Topic,
		Subject:           reqs.Subject,
		TotalPages:        reqs.TotalPages,
		Audience:          reqs.TargetAudience,
		GlobalStyle:       reqs.GlobalStyle,
		KnowledgePoints:   reqs.KnowledgePoints,
		TeachingGoals:     reqs.TeachingGoals,
		TeachingLogic:     reqs.TeachingLogic,
		KeyDifficulties:   reqs.KeyDifficulties,
		Duration:          reqs.Duration,
		InteractionDesign: reqs.InteractionDesign,
		OutputFormats:     reqs.OutputFormats,
		ReferenceFiles:    reqs.ReferenceFiles,
	}
	p.executor.Execute(action, sessionCtx, p.EnqueueContext)
}
