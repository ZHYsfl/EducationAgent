package agent

import (
	"context"

	"voiceagentv2/internal/executor"
	"voiceagentv2/internal/protocol"
)

// thinkLoop feeds partial ASR text into the LLM to warm the KV cache.
// All generated tokens are discarded.
func (p *Pipeline) thinkLoop(ctx context.Context) {
	var lastThinkLen int

	deltaChars := p.config.ThinkDeltaChars
	if deltaChars <= 0 {
		deltaChars = 7
	}

	for {
		select {
		case userInput := <-p.userInputCh:
			inputLen := len([]rune(userInput))
			if inputLen < lastThinkLen+deltaChars {
				continue
			}
			p.cancelThinkStream()
			streamCtx, cancel := context.WithCancel(ctx)
			p.setThinkCancel(cancel)
			go p.runThinkStream(streamCtx, userInput)
			lastThinkLen = inputLen

		case <-ctx.Done():
			p.cancelThinkStream()
			return
		}
	}
}

func (p *Pipeline) runThinkStream(ctx context.Context, userInput string) {
	systemPrompt := p.buildSystemPrompt()
	messages := p.history.ToOpenAIWithDraftThoughtAndPrompt(userInput, "", systemPrompt)
	for range p.largeLLM.StreamChat(ctx, messages) {
	}
}

func (p *Pipeline) executeAction(_ context.Context, action protocol.Action) {
	reqs := p.session.GetRequirements()
	if reqs == nil {
		reqs = &TaskRequirements{}
	}
	p.session.activeTaskMu.RLock()
	activeTask := p.session.ActiveTaskID
	viewingPage := p.session.ViewingPageID
	baseTS := p.session.LastVADTimestamp
	p.session.activeTaskMu.RUnlock()

	sessionCtx := executor.SessionContext{
		UserID:            p.session.UserID,
		SessionID:         p.session.SessionID,
		ActiveTaskID:      activeTask,
		ViewingPageID:     viewingPage,
		BaseTimestamp:     baseTS,
		Topic:             reqs.Topic,
		Subject:           reqs.Description,
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
