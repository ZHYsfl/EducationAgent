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

	// Grab accumulated thinker output before clearing
	previousThought := p.getDraftOutput()
	p.resetDraftOutput()

	p.history.AddUser(userText)

	contextMsgs := p.drainContextQueue()
	contextPrompt := FormatContextForLLM(contextMsgs)

	systemPrompt := p.config.SystemPrompt
	p.session.reqMu.RLock()
	reqSnapshot := CloneTaskRequirements(p.session.Requirements)
	p.session.reqMu.RUnlock()
	if reqSnapshot != nil && (reqSnapshot.Status == "collecting" || reqSnapshot.Status == "confirming") {
		var profile *UserProfile
		if p.clients != nil {
			if pInfo, err := p.clients.GetUserProfile(ctx, reqSnapshot.UserID); err == nil {
				profile = &pInfo
			}
		}
		systemPrompt = reqSnapshot.BuildRequirementsSystemPrompt(profile)
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

	// Add protocol instructions
	systemPrompt += `

## 动作协议
执行操作时使用: @{type|key:value|key:value}
内部思考使用: #{思考内容}（可选，不显示给用户）

支持的动作:
- update_requirements: @{update_requirements|字段:值} - 更新需求信息（包括初始化topic）
- ppt_init: @{ppt_init|topic:主题|desc:描述} - 开始制作PPT（需先收集完所有必填信息）
- ppt_mod: @{ppt_mod|task:任务ID|raw_text:用户原话} - 修改PPT（task参数可选，默认使用当前活跃任务；多任务时必须明确指定）
- kb_query: @{kb_query|query:查询内容}
- web_search: @{web_search|query:搜索关键词}

## 需求收集流程
1. 用户提出制作PPT需求 → @{update_requirements|topic:主题}
2. 逐步询问用户（每次1-2个问题），收集信息后 → @{update_requirements|字段:值}
3. 所有必填信息收集完成 → @{ppt_init|topic:主题|desc:描述}

必填字段（12个）: topic, subject, audience, total_pages, knowledge_points, teaching_goals, teaching_logic, key_difficulties, duration, global_style, interaction_design, output_formats

## PPT修改流程
- 单任务场景: @{ppt_mod|raw_text:用户原话} （task参数可省略）
- 多任务场景: 根据用户提到的任务主题，明确指定task参数 @{ppt_mod|task:任务ID|raw_text:用户原话}

示例:
用户: "帮我做个高等数学的PPT"
你: 好的。@{update_requirements|topic:高等数学} 请问目标听众是谁？
用户: "大学生"
你: 明白了。@{update_requirements|audience:大学生} 需要多少页？

用户: "把物理课件的第3页改成蓝色"
你: 好的。@{ppt_mod|task:task_physics_id|raw_text:把第3页改成蓝色}
`

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

	// Stream tokens from Large LLM
	messages := p.history.ToOpenAIWithThoughtAndPrompt(previousThought, systemPrompt)
	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	totalTokens := 0
	var sentenceBuf strings.Builder
	var allTokens strings.Builder
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

		p.postProcessResponse(ctx, userText, finalText)
		p.asyncExtractMemory(userText, finalText)

		p.session.SetState(StateIdle)
	}

	p.adaptive.Adjust()
	p.adaptive.Save(p.config.AdaptiveSizesFile)
}

func (p *Pipeline) postProcessResponse(ctx context.Context, userText, llmResponse string) {
	if p.tryResolveConflict(ctx, userText, llmResponse) {
		return
	}

	// Tool calling 已在流式输出过程中自动处理
}
