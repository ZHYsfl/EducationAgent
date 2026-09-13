// Package voiceengine is the interruptible voice-dialog concurrency core:
// a per-turn Producer streams LLM tokens stamped with a generation counter,
// while a barge-in bumps the generation and mutes everything older.
package voiceengine

// TokenState is the wire state string of API.md 4.1 carried on every token.
type TokenState string

const (
	StateIdle           TokenState = "idle"
	StateTTSTokens      TokenState = "inferring_tts_tokens"
	StateToolCallTokens TokenState = "inferring_tool_call_tokens"
)

// Token is one streamed unit on its way to TTS. Gen is the generation the
// token belongs to; consumers drop any token whose Gen is not current.
type Token struct {
	Content string
	State   TokenState
	Gen     int64
}
