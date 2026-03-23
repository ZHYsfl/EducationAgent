package history

import (
	"sync"

	"github.com/openai/openai-go/v3"
)

type ConversationHistory struct {
	systemPrompt string
	messages     []HistoryMessage
	mu           sync.RWMutex
}

type HistoryMessage struct {
	Role    string // "user", "assistant"
	Content string
}

func NewConversationHistory(systemPrompt string) *ConversationHistory {
	return &ConversationHistory{
		systemPrompt: systemPrompt,
	}
}

func (h *ConversationHistory) AddUser(content string) {
	h.mu.Lock()
	h.messages = append(h.messages, HistoryMessage{Role: "user", Content: content})
	h.mu.Unlock()
}

func (h *ConversationHistory) AddAssistant(content string) {
	h.mu.Lock()
	h.messages = append(h.messages, HistoryMessage{Role: "assistant", Content: content})
	h.mu.Unlock()
}

func (h *ConversationHistory) AddInterruptedAssistant(content string) {
	h.mu.Lock()
	h.messages = append(h.messages, HistoryMessage{
		Role:    "assistant",
		Content: content + " [被打断]",
	})
	h.mu.Unlock()
}

// Messages returns a snapshot of the conversation messages (excluding system prompt).
func (h *ConversationHistory) Messages() []HistoryMessage {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]HistoryMessage, len(h.messages))
	copy(out, h.messages)
	return out
}

// ToOpenAI converts history to openai-go message format for API calls.
func (h *ConversationHistory) ToOpenAI() []openai.ChatCompletionMessageParamUnion {
	h.mu.RLock()
	defer h.mu.RUnlock()

	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(h.messages)+1)
	msgs = append(msgs, openai.SystemMessage(h.systemPrompt))

	for _, m := range h.messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, openai.UserMessage(m.Content))
		case "assistant":
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		}
	}
	return msgs
}

// ToOpenAIWithDraftAndThought builds messages for draft thinking rounds:
// history + partial user text + previous thinker output (if any).
func (h *ConversationHistory) ToOpenAIWithDraftAndThought(draftUserText, previousThought string) []openai.ChatCompletionMessageParamUnion {
	msgs := h.ToOpenAI()
	msgs = append(msgs, openai.UserMessage(draftUserText))
	if previousThought != "" {
		msgs = append(msgs, openai.AssistantMessage("[内部草稿，可能不完整或片面，仅供继续推理，不可直接复述] "+previousThought+" [思考中...]"))
	}
	return msgs
}

func (h *ConversationHistory) ToOpenAIWithThoughtAndPrompt(previousThought, systemPrompt string) []openai.ChatCompletionMessageParamUnion {
	h.mu.RLock()
	defer h.mu.RUnlock()

	prompt := h.systemPrompt
	if systemPrompt != "" {
		prompt = systemPrompt
	}
	if previousThought != "" {
		prompt += "\n\n[预思考草稿 - 基于用户部分输入生成，可能方向有误，仅供参考，以用户完整输入为准]\n" + previousThought
	}

	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(h.messages)+1)
	msgs = append(msgs, openai.SystemMessage(prompt))
	for _, m := range h.messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, openai.UserMessage(m.Content))
		case "assistant":
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		}
	}
	return msgs
}
