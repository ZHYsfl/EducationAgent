package agent

import (
	"context"
	"log"
)

// maybeCompressHistory pushes the oldest 1/3 history to memory service
// when local history grows beyond threshold, then trims local history.
func (p *Pipeline) maybeCompressHistory() {
	if p.clients == nil {
		return
	}
	if p.history.TotalChars() <= 8000 {
		return
	}

	msgs := p.history.Messages()
	cut := len(msgs) / 3
	if cut <= 0 {
		return
	}

	turns := make([]ConversationTurn, 0, cut)
	for _, m := range msgs[:cut] {
		turns = append(turns, ConversationTurn{Role: m.Role, Content: m.Content})
	}
	if len(turns) == 0 {
		return
	}

	if err := p.clients.PushContext(context.Background(), PushContextRequest{
		UserID:    p.session.UserID,
		SessionID: p.session.SessionID,
		Messages:  turns,
	}); err != nil {
		log.Printf("[history] compress push failed: %v", err)
		return
	}

	p.history.DeleteFront(cut)
	p.session.memoryMu.Lock()
	p.session.lastMemoryPushIdx = 0
	p.session.memoryMu.Unlock()
	log.Printf("[history] compressed and pushed %d messages", cut)
}
