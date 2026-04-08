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
	Role    string // "user", "assistant", "tool"
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

func (h *ConversationHistory) AddTool(content string) {
	h.mu.Lock()
	h.messages = append(h.messages, HistoryMessage{Role: "tool", Content: content})
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
		Content: content + "</interrupted>",
	})
	h.mu.Unlock()
}

// TotalChars returns the total character count of all message contents.
func (h *ConversationHistory) TotalChars() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, m := range h.messages {
		n += len(m.Content)
	}
	return n
}

// DeleteFront removes the first n messages from history.
func (h *ConversationHistory) DeleteFront(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if n <= 0 || n > len(h.messages) {
		return
	}
	h.messages = h.messages[n:]
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
	return h.messagesWithSystemLocked(h.systemPrompt)
}

// messagesWithSystemLocked returns [system, ...history]. Caller must hold h.mu (read lock).
func (h *ConversationHistory) messagesWithSystemLocked(system string) []openai.ChatCompletionMessageParamUnion {
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(h.messages)+1)
	msgs = append(msgs, openai.SystemMessage(system))
	for _, m := range h.messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, openai.UserMessage(m.Content))
		case "assistant":
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		case "tool":
			msgs = append(msgs, openai.UserMessage("<tool>"+m.Content+"</tool>"))
		}
	}
	return msgs
}

// ToOpenAIWithDraftAndThought builds messages for draft thinking rounds.
func (h *ConversationHistory) ToOpenAIWithDraftAndThought(draftUserText, previousThought string) []openai.ChatCompletionMessageParamUnion {
	return h.ToOpenAIWithDraftThoughtAndPrompt(draftUserText, previousThought, "")
}

// ToOpenAIWithThoughtAndPrompt builds the final reply request after the user turn is finalized.
// previousThought is injected as an assistant prefill so the model continues from the draft.
func (h *ConversationHistory) ToOpenAIWithThoughtAndPrompt(previousThought, systemPrompt string) []openai.ChatCompletionMessageParamUnion {
	h.mu.RLock()
	defer h.mu.RUnlock()

	prompt := h.systemPrompt
	if systemPrompt != "" {
		prompt = systemPrompt
	}
	msgs := h.messagesWithSystemLocked(prompt)
	if previousThought != "" {
		msgs = append(msgs, openai.AssistantMessage(previousThought))
	}
	return msgs
}

// ToOpenAIWithDraftThoughtAndPrompt builds draft-thinking messages without mutating
// conversation history: [system(runtime), ...history, user(draft), assistant(previousThought?)].
func (h *ConversationHistory) ToOpenAIWithDraftThoughtAndPrompt(draftUserText, previousThought, systemPrompt string) []openai.ChatCompletionMessageParamUnion {
	h.mu.RLock()
	defer h.mu.RUnlock()

	prompt := h.systemPrompt
	if systemPrompt != "" {
		prompt = systemPrompt
	}
	msgs := h.messagesWithSystemLocked(prompt)
	msgs = append(msgs, openai.UserMessage(draftUserText))
	if previousThought != "" {
		msgs = append(msgs, openai.AssistantMessage(previousThought))
	}
	return msgs
}
