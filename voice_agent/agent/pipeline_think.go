package agent

import (
	"context"
	"strings"
	"time"

	"voiceagent/internal/executor"
	"voiceagent/internal/protocol"
)

// ---------------------------------------------------------------------------
// O/T/A Concurrent Loops
// ---------------------------------------------------------------------------

func (p *Pipeline) thinkLoop(ctx context.Context) {
	var lastThinkLen int
	var lastThinkAt time.Time
	isWarmup := true
	const warmupMinChars = 7
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

			if inputLen < warmupMinChars {
				p.cancelThinkStream()
				streamCtx, cancel := context.WithCancel(ctx)
				p.setThinkCancel(cancel)
				isWarmup = true
				go p.runThinkStream(streamCtx, userInput, true)
				lastThinkLen = inputLen
				continue
			}

			if isWarmup {
				p.cancelThinkStream()
				streamCtx, cancel := context.WithCancel(ctx)
				p.setThinkCancel(cancel)
				isWarmup = false
				go p.runThinkStream(streamCtx, userInput, false)
				lastThinkLen = inputLen
				lastThinkAt = now
				continue
			}

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

func (p *Pipeline) runThinkStream(ctx context.Context, userInput string, warmup bool) {
	systemPrompt := p.buildFullSystemPrompt(ctx, false)
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

func (p *Pipeline) outputLoop(ctx context.Context) {
	var sentenceBuf strings.Builder

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
	p.thinkDraftRaw.WriteString(token)
	p.thinkDraftMu.Unlock()
}

func (p *Pipeline) consumeThinkDraft() string {
	p.thinkDraftMu.Lock()
	defer p.thinkDraftMu.Unlock()
	out := p.thinkDraftRaw.String()
	p.thinkDraftRaw.Reset()
	return out
}

func (p *Pipeline) resetThinkDraft() {
	p.thinkDraftMu.Lock()
	p.thinkDraftRaw.Reset()
	p.thinkDraftMu.Unlock()
}

func (p *Pipeline) activateThinkActionGuard() {
	guardMS := p.config.ThinkActionGuardMS
	if guardMS <= 0 {
		guardMS = 1500
	}
	p.thinkGuardMu.Lock()
	p.thinkGuardTo = time.Now().Add(time.Duration(guardMS) * time.Millisecond)
	p.thinkGuardMu.Unlock()
}

func (p *Pipeline) isThinkActionGuarded(now time.Time) bool {
	p.thinkGuardMu.RLock()
	defer p.thinkGuardMu.RUnlock()
	return now.Before(p.thinkGuardTo)
}
