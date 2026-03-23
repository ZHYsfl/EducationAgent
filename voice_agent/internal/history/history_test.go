package history

import (
	"testing"
)

func TestConversationHistory_AddAndRetrieve(t *testing.T) {
	h := NewConversationHistory("system")
	h.AddUser("hello")
	h.AddAssistant("hi there")
	h.AddInterruptedAssistant("partial")

	msgs := h.ToOpenAI()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (system + 3), got %d", len(msgs))
	}
}

func TestConversationHistory_ToOpenAIWithThoughtAndPrompt(t *testing.T) {
	h := NewConversationHistory("default system")
	h.AddUser("question")

	msgs := h.ToOpenAIWithThoughtAndPrompt("draft thought", "custom system")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
}

func TestConversationHistory_ToOpenAIWithDraftAndThought(t *testing.T) {
	h := NewConversationHistory("sys")
	h.AddUser("prev question")
	h.AddAssistant("prev answer")

	msgs := h.ToOpenAIWithDraftAndThought("partial text", "some thinking")
	// system + prev_user + prev_assistant + draft_user + draft_assistant_thought
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}
}

func TestConversationHistory_NoPreviousThought(t *testing.T) {
	h := NewConversationHistory("sys")
	h.AddUser("prev")

	msgs := h.ToOpenAIWithDraftAndThought("partial", "")
	// system + prev_user + draft_user (no thought)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}
