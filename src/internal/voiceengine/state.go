package voiceengine

import (
	"context"
	"strings"
	"sync/atomic"
)

const (
	LLMStateIdle int32 = 0
	LLMStateTTS  int32 = 1
	LLMStateTool int32 = 2
)

const defaultQueueCapacity = 256

// Engine owns the cross-turn shared state: the generation counter (bumped on
// every barge-in), the LLM phase, the outbound token queue and the tool
// record of the current turn.
type Engine struct {
	serverCtx  context.Context
	generation atomic.Int64
	llmState   atomic.Int32

	buf      sentenceBuf
	queue    *TokenQueue
	toolInfo ToolInfo
}

type EngineOption func(*Engine)

// WithQueueCapacity overrides the bounded token queue capacity (default 256).
func WithQueueCapacity(n int) EngineOption {
	return func(e *Engine) {
		if n > 0 {
			e.queue = NewTokenQueue(n)
		}
	}
}

func NewEngine(serverCtx context.Context, opts ...EngineOption) *Engine {
	e := &Engine{
		serverCtx: serverCtx,
		queue:     NewTokenQueue(defaultQueueCapacity),
	}
	e.buf.gen = -1
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// BumpGeneration seals the current utterance: every token stamped with an
// older generation becomes mute on the consumer side.
func (e *Engine) BumpGeneration() {
	e.generation.Add(1)
}

func (e *Engine) Generation() int64 {
	return e.generation.Load()
}

func (e *Engine) SetLLMState(s int32) {
	e.llmState.Store(s)
}

func (e *Engine) LLMState() int32 {
	return e.llmState.Load()
}

// Queue is the producer-to-consumer token channel.
func (e *Engine) Queue() *TokenQueue {
	return e.queue
}

// ToolInfo accumulates the tool calls executed during the current turn;
// compose reads it after <-done.
func (e *Engine) ToolInfo() *ToolInfo {
	return &e.toolInfo
}

// HandleToken feeds one token to the sentence buffer, validating it against
// the current generation. ok is true when this token completed a sentence.
func (e *Engine) HandleToken(t Token) (string, int64, bool) {
	return e.buf.HandleToken(t, e.generation.Load())
}

// ResetBuf clears the half-sentence left in the buffer (barge-in A-knife).
func (e *Engine) ResetBuf() {
	e.buf.Reset()
}

// FlushSentence takes the buffered half-sentence, if any, and clears the
// buffer. Used when the idle token ends a turn whose last sentence had no
// closing punctuation.
func (e *Engine) FlushSentence() (string, bool) {
	s := e.buf.Snapshot()
	if s == "" {
		return "", false
	}
	e.buf.Reset()
	return s, true
}

// DrainLeftUnsaid returns the half-sentence plus every batch still queued,
// then clears both. Diagnostic only; after BumpGeneration the content is
// sealed anyway.
func (e *Engine) DrainLeftUnsaid() string {
	var sb strings.Builder
	sb.WriteString(e.buf.Snapshot())
	for _, batch := range e.queue.Drain() {
		for _, t := range batch {
			sb.WriteString(t.Content)
		}
	}
	e.buf.Reset()
	return sb.String()
}

// LLMStateString maps the internal phase to the API wire string (1.1/4.1).
func LLMStateString(s int32) string {
	switch s {
	case LLMStateTTS:
		return string(StateTTSTokens)
	case LLMStateTool:
		return string(StateToolCallTokens)
	default:
		return string(StateIdle)
	}
}
