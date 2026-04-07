package history_test

import (
	"encoding/json"
	"strings"
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
	raw, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "hello") || !strings.Contains(s, "interrupted") {
		t.Fatalf("expected user and interrupted assistant content in payload: %s", s)
	}
}

func TestConversationHistory_ToOpenAIWithThoughtAndPrompt(t *testing.T) {
	h := history.NewConversationHistory("default system")
	h.AddUser("question")

	msgs := h.ToOpenAIWithThoughtAndPrompt("draft thought", "custom system")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	raw, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "custom system") {
		t.Fatalf("expected runtime system prompt in first message: %s", s)
	}
	if !strings.Contains(s, "draft thought") {
		t.Fatalf("expected previous thought appended to system: %s", s)
	}
}

func TestConversationHistory_ToOpenAIWithThoughtAndPrompt_FallbackSystem(t *testing.T) {
	h := history.NewConversationHistory("stored-only")
	h.AddUser("q")

	msgs := h.ToOpenAIWithThoughtAndPrompt("prior", "")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	raw, _ := json.Marshal(msgs)
	s := string(raw)
	if !strings.Contains(s, "stored-only") || !strings.Contains(s, "prior") {
		t.Fatalf("empty runtime systemPrompt should use h.systemPrompt and still append thought: %s", s)
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
	raw, _ := json.Marshal(msgs)
	s := string(raw)
	if !strings.Contains(s, "partial text") || !strings.Contains(s, "some thinking") {
		t.Fatalf("expected partial user + prior thought: %s", s)
	}
}

func TestConversationHistory_NoPreviousThought(t *testing.T) {
	h := history.NewConversationHistory("sys")
	h.AddUser("prev")

	msgs := h.ToOpenAIWithDraftAndThought("partial", "")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	raw, _ := json.Marshal(msgs)
	if !strings.Contains(string(raw), "partial") {
		t.Fatalf("expected partial user message: %s", string(raw))
	}
}

func TestConversationHistory_ToOpenAIWithDraftThoughtAndPrompt(t *testing.T) {
	h := history.NewConversationHistory("stored")
	h.AddUser("u1")

	msgs := h.ToOpenAIWithDraftThoughtAndPrompt("draft", "t1", "runtime")
	raw, _ := json.Marshal(msgs)
	s := string(raw)
	if !strings.Contains(s, "runtime") || !strings.Contains(s, "draft") || !strings.Contains(s, "t1") {
		t.Fatalf("unexpected payload: %s", s)
	}
}
