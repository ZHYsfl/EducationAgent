package history_test

import (
	"testing"

	"voiceagent/internal/history"
)

func TestConversationHistory_AddAndRetrieve(t *testing.T) {
	h := history.NewConversationHistory("system")
	h.AddUser("hello")
	h.AddAssistant("hi there")
	h.AddInterruptedAssistant("partial")

	msgs := h.ToOpenAI()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (system + 3), got %d", len(msgs))
	}
}

func TestConversationHistory_ToOpenAIWithThoughtAndPrompt(t *testing.T) {
	h := history.NewConversationHistory("default system")
	h.AddUser("question")

	msgs := h.ToOpenAIWithThoughtAndPrompt("draft thought", "custom system")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
}

func TestConversationHistory_ToOpenAIWithDraftAndThought(t *testing.T) {
	h := history.NewConversationHistory("sys")
	h.AddUser("prev question")
	h.AddAssistant("prev answer")

	msgs := h.ToOpenAIWithDraftAndThought("partial text", "some thinking")
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}
}

func TestConversationHistory_NoPreviousThought(t *testing.T) {
	h := history.NewConversationHistory("sys")
	h.AddUser("prev")

	msgs := h.ToOpenAIWithDraftAndThought("partial", "")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}
