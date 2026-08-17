package agent_runtime

import (
	"github.com/openai/openai-go/v3"
)

// MemoryMode controls how conversation history is trimmed before being sent
// to the LLM.
type MemoryMode int

const (
	// MemoryModeNone passes messages through unchanged.
	MemoryModeNone MemoryMode = iota
	// MemoryModeSlideWindow keeps the system prompt plus the most recent N
	// user/assistant/tool messages.
	MemoryModeSlideWindow
	// MemoryModeCompress summarizes or truncates older messages when the
	// estimated input length exceeds a threshold.
	MemoryModeCompress
)

// MemoryManager prepares messages for the LLM call.
type MemoryManager interface {
	Prepare(messages []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion
}

// defaultMemory is a no-op memory manager.
type defaultMemory struct{}

func (m *defaultMemory) Prepare(messages []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	return messages
}

// slideWindowMemory keeps the system prompt plus the last N non-system messages.
type slideWindowMemory struct {
	maxMessages int
}

func (m *slideWindowMemory) Prepare(messages []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	if m.maxMessages <= 0 {
		return messages
	}

	var system openai.ChatCompletionMessageParamUnion
	var nonSystem []openai.ChatCompletionMessageParamUnion
	for _, msg := range messages {
		if isSystemMessage(msg) {
			system = msg
			continue
		}
		nonSystem = append(nonSystem, msg)
	}

	if len(nonSystem) > m.maxMessages {
		nonSystem = nonSystem[len(nonSystem)-m.maxMessages:]
	}

	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	if system.OfSystem != nil {
		out = append(out, system)
	}
	out = append(out, nonSystem...)
	return out
}

// compressMemory truncates older messages when the estimated input length
// exceeds maxInputTokens. In a future iteration this can be replaced with a
// real summarizer.
type compressMemory struct {
	maxInputTokens int
}

func (m *compressMemory) Prepare(messages []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	if m.maxInputTokens <= 0 {
		return messages
	}

	const overhead = 16
	estimate := func(msgs []openai.ChatCompletionMessageParamUnion) int {
		total := 0
		for _, msg := range msgs {
			total += overhead + estimateMessageLength(msg)
		}
		return total
	}

	if estimate(messages) <= m.maxInputTokens {
		return messages
	}

	// Drop oldest non-system messages until we fit.
	var system openai.ChatCompletionMessageParamUnion
	var nonSystem []openai.ChatCompletionMessageParamUnion
	for _, msg := range messages {
		if isSystemMessage(msg) {
			system = msg
			continue
		}
		nonSystem = append(nonSystem, msg)
	}

	for len(nonSystem) > 1 && estimate(append(sliceMsg(system), nonSystem...)) > m.maxInputTokens {
		nonSystem = nonSystem[1:]
	}

	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	if system.OfSystem != nil {
		out = append(out, system)
	}
	out = append(out, nonSystem...)
	return out
}

func isSystemMessage(msg openai.ChatCompletionMessageParamUnion) bool {
	return msg.OfSystem != nil
}

func sliceMsg(msg openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	if msg.OfSystem != nil {
		return []openai.ChatCompletionMessageParamUnion{msg}
	}
	return nil
}

// estimateMessageLength returns a rough length estimate for a message. It uses
// rune count plus a small overhead. This can be swapped for real token counting
// later without changing callers.
func estimateMessageLength(msg openai.ChatCompletionMessageParamUnion) int {
	total := 0
	switch {
	case msg.OfSystem != nil:
		if msg.OfSystem.Content.OfString.Valid() {
			return len([]rune(msg.OfSystem.Content.OfString.Value))
		}
		for _, p := range msg.OfSystem.Content.OfArrayOfContentParts {
			total += len([]rune(p.Text))
		}
	case msg.OfUser != nil:
		if msg.OfUser.Content.OfString.Valid() {
			return len([]rune(msg.OfUser.Content.OfString.Value))
		}
		for _, p := range msg.OfUser.Content.OfArrayOfContentParts {
			if p.OfText != nil {
				total += len([]rune(p.OfText.Text))
			}
		}
	case msg.OfAssistant != nil:
		if msg.OfAssistant.Content.OfString.Valid() {
			return len([]rune(msg.OfAssistant.Content.OfString.Value))
		}
		for _, p := range msg.OfAssistant.Content.OfArrayOfContentParts {
			if p.OfText != nil {
				total += len([]rune(p.OfText.Text))
			}
		}
	case msg.OfTool != nil:
		if msg.OfTool.Content.OfString.Valid() {
			return len([]rune(msg.OfTool.Content.OfString.Value))
		}
		for _, p := range msg.OfTool.Content.OfArrayOfContentParts {
			total += len([]rune(p.Text))
		}
	}
	return total
}

// WithMemoryMode sets the memory management strategy. The optional max argument
// is used by MemoryModeSlideWindow (max non-system messages) and
// MemoryModeCompress (max estimated input tokens).
func WithMemoryMode(mode MemoryMode, max int) AgentOption {
	return func(a *Agent) {
		switch mode {
		case MemoryModeSlideWindow:
			a.memory = &slideWindowMemory{maxMessages: max}
		case MemoryModeCompress:
			a.memory = &compressMemory{maxInputTokens: max}
		default:
			a.memory = &defaultMemory{}
		}
	}
}
