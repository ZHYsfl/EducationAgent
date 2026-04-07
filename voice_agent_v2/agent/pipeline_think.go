package agent

import (
	"context"
	"time"

	"voiceagentv2/internal/executor"
	"voiceagentv2/internal/protocol"
)

// ---------------------------------------------------------------------------
// Think Loop — KV-cache warmup + speculative draft generation
// ---------------------------------------------------------------------------

// thinkLoop runs during listening: feeds partial ASR text into the LLM to warm
// the KV cache. Once text is long enough it starts generating a speculative draft.
func (p *Pipeline) thinkLoop(ctx context.Context) {
	var lastThinkLen int
	var lastThinkAt time.Time
	isWarmup := true

	deltaChars := p.config.ThinkDeltaChars
	if deltaChars <= 0 {
		deltaChars = 7
	}
	minInterval := time.Duration(p.config.ThinkMinIntervalMS) * time.Millisecond
	if minInterval <= 0 {
		minInterval = 800 * time.Millisecond
	}

	for {
		select {
		case userInput := <-p.userInputCh:
			inputLen := len([]rune(userInput))
			now := time.Now()

			if inputLen < deltaChars {
				// Too short: warmup only (no draft tokens forwarded)
				p.cancelThinkStream()
				streamCtx, cancel := context.WithCancel(ctx)
				p.setThinkCancel(cancel)
				isWarmup = true
				go p.runThinkStream(streamCtx, userInput, true)
				lastThinkLen = inputLen
				continue
			}

			if isWarmup {
				// First real-length input: start draft generation
				p.cancelThinkStream()
				streamCtx, cancel := context.WithCancel(ctx)
				p.setThinkCancel(cancel)
				isWarmup = false
				go p.runThinkStream(streamCtx, userInput, false)
				lastThinkLen = inputLen
				lastThinkAt = now
				continue
			}

			// Throttle: only re-launch if enough new chars and enough time passed
			if inputLen >= lastThinkLen+deltaChars {
				if !lastThinkAt.IsZero() && now.Sub(lastThinkAt) < minInterval {
					continue
				}
				if p.isThinkActionGuarded(now) {
					continue
				}
				p.cancelThinkStream()
				streamCtx, cancel := context.WithCancel(ctx)
				p.setThinkCancel(cancel)
				go p.runThinkStream(streamCtx, userInput, false)
				lastThinkLen = inputLen
				lastThinkAt = now
			}

		case <-ctx.Done():
			p.cancelThinkStream()
			return
		}
	}
}

// runThinkStream streams from the LLM with the draft user text.
// warmup=true: tokens are discarded (KV cache only). warmup=false: tokens go to tokenCh.
func (p *Pipeline) runThinkStream(ctx context.Context, userInput string, warmup bool) {
	systemPrompt := p.buildSystemPrompt(false)
	messages := p.history.ToOpenAIWithDraftThoughtAndPrompt(userInput, "", systemPrompt)
	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	for token := range tokenCh {
		if warmup {
			continue
		}
		p.appendThinkDraftToken(token)
		select {
		case p.tokenCh <- token:
		case <-ctx.Done():
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Output Loop — parses tokens, dispatches actions, routes visible text to TTS
// ---------------------------------------------------------------------------

// outputLoop reads from tokenCh, strips protocol markers, executes actions,
// and forwards visible sentences to sentenceCh for TTS.
func (p *Pipeline) outputLoop(ctx context.Context) {
	for {
		select {
		case token := <-p.tokenCh:
			result := p.parser.Feed(token)

			for _, action := range result.Actions {
				go p.executeAction(ctx, action)
			}
			if len(result.Actions) > 0 || result.HasOpenAction {
				p.activateThinkActionGuard()
			}

			if result.VisibleText != "" {
				if p.session.GetState() == StateListening {
					continue
				}
				p.session.SendJSON(WSMessage{Type: "response", Text: result.VisibleText})
			}

		case <-ctx.Done():
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Action Execution
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Think stream helpers
// ---------------------------------------------------------------------------

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

func (p *Pipeline) appendThinkDraftToken(token string) {
	p.thinkDraftMu.Lock()
	p.thinkDraft.WriteString(token)
	p.thinkDraftMu.Unlock()
}

func (p *Pipeline) consumeThinkDraft() string {
	p.thinkDraftMu.Lock()
	defer p.thinkDraftMu.Unlock()
	out := p.thinkDraft.String()
	p.thinkDraft.Reset()
	return out
}

func (p *Pipeline) resetThinkDraft() {
	p.thinkDraftMu.Lock()
	p.thinkDraft.Reset()
	p.thinkDraftMu.Unlock()
}

func (p *Pipeline) activateThinkActionGuard() {
	guardMS := p.config.ThinkActionGuardMS
	if guardMS <= 0 {
		guardMS = 1500
	}
	p.thinkGuardMu.Lock()
	p.thinkGuardUntil = time.Now().Add(time.Duration(guardMS) * time.Millisecond)
	p.thinkGuardMu.Unlock()
}

func (p *Pipeline) isThinkActionGuarded(now time.Time) bool {
	p.thinkGuardMu.RLock()
	defer p.thinkGuardMu.RUnlock()
	return now.Before(p.thinkGuardUntil)
}
