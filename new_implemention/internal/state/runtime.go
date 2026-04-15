package state

import (
	"context"
	"errors"
	"sync"
)

// ErrAlreadyRunning is returned by Start when the runtime is already active.
var ErrAlreadyRunning = errors.New("runtime already running")

// PPTAgentRuntime manages the background goroutine for the PPT agent.
type PPTAgentRuntime struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	wg      sync.WaitGroup
}

// NewPPTAgentRuntime creates a new runtime manager.
func NewPPTAgentRuntime() *PPTAgentRuntime {
	return &PPTAgentRuntime{}
}

// Start launches the given function in a new cancellable goroutine.
// Returns an error if the runtime is already running.
func (r *PPTAgentRuntime) Start(fn func(ctx context.Context)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return ErrAlreadyRunning
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.running = true
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn(ctx)
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()
	return nil
}

// Stop cancels the current context. It does NOT wait for the goroutine to finish
// so that it can be safely called from inside the goroutine (e.g. a tool function).
// Callers that need to synchronise should call Wait() afterwards.
func (r *PPTAgentRuntime) Stop() {
	r.mu.Lock()
	if !r.running || r.cancel == nil {
		r.mu.Unlock()
		return
	}
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	cancel()
}

// Wait blocks until the goroutine started by Start exits.
func (r *PPTAgentRuntime) Wait() {
	r.wg.Wait()
}

// IsRunning reports whether the runtime goroutine is active.
func (r *PPTAgentRuntime) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}
