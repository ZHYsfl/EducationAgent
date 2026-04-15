package voiceagent

import (
	"context"
	"fmt"
)

// ActionFunc is the signature for a voice-agent action handler.
type ActionFunc func(ctx context.Context, args map[string]string) (string, error)

// Executor maps action names to Go functions and runs them.
type Executor struct {
	actions map[string]ActionFunc
}

// NewExecutor creates an empty executor.
func NewExecutor() *Executor {
	return &Executor{
		actions: make(map[string]ActionFunc),
	}
}

// Register binds an action name to its handler.
func (e *Executor) Register(name string, fn ActionFunc) {
	e.actions[name] = fn
}

// Execute parses the payload, looks up the handler, and invokes it.
func (e *Executor) Execute(ctx context.Context, payload string) (string, error) {
	name, args, err := ParseAction(payload)
	if err != nil {
		return "", fmt.Errorf("parse action: %w", err)
	}
	fn, ok := e.actions[name]
	if !ok {
		return "", fmt.Errorf("unknown action: %s", name)
	}
	return fn(ctx, args)
}
