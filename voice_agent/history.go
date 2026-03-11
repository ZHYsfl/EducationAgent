package main

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

// ToOpenAIWithDraft returns messages with an additional user message appended
// (used for draft thinking during listening).
func (h *ConversationHistory) ToOpenAIWithDraft(draftUserText string) []openai.ChatCompletionMessageParamUnion {
	msgs := h.ToOpenAI()
	msgs = append(msgs, openai.UserMessage(draftUserText))
	return msgs
}
