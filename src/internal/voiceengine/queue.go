package voiceengine

import "context"

// TokenQueue is the bounded batch channel between producer and consumer.
type TokenQueue struct {
	ch chan []Token
}

func NewTokenQueue(capacity int) *TokenQueue {
	if capacity <= 0 {
		capacity = defaultQueueCapacity
	}
	return &TokenQueue{ch: make(chan []Token, capacity)}
}

// Push blocks until the batch is accepted or ctx is done. No default branch
// on purpose: a full queue must apply backpressure, never silently drop.
func (q *TokenQueue) Push(ctx context.Context, batch []Token) error {
	select {
	case q.ch <- batch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Chan is the consumer side.
func (q *TokenQueue) Chan() <-chan []Token {
	return q.ch
}

// Drain removes and returns every pending batch without blocking.
func (q *TokenQueue) Drain() [][]Token {
	var out [][]Token
	for {
		select {
		case b := <-q.ch:
			out = append(out, b)
		default:
			return out
		}
	}
}
