package state

import (
	"context"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
)

func userMessage(content string) openai.ChatCompletionMessageParamUnion {
	return openai.UserMessage(content)
}

func assistantMessage(content string) openai.ChatCompletionMessageParamUnion {
	return openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			Content: openai.ChatCompletionAssistantMessageParamContentUnion{
				OfString: openai.String(content),
			},
		},
	}
}

func TestVoiceHistory(t *testing.T) {
	s := NewAppState()
	s.AppendVoiceHistory(userMessage("hello"))
	s.AppendVoiceHistory(assistantMessage("hi"))

	hist := s.GetVoiceHistory()
	if len(hist) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(hist))
	}
}

func TestResetConversationClearsHistory(t *testing.T) {
	s := NewAppState()
	s.AppendVoiceHistory(userMessage("hello"))
	s.SetConversationStarted()

	s.ResetConversation()
	if len(s.GetVoiceHistory()) != 0 {
		t.Fatalf("expected empty history after reset")
	}
	if s.IsConversationStarted() {
		t.Fatalf("expected conversation not started after reset")
	}
}

func TestWaitingEpisodeCompletes(t *testing.T) {
	s := NewAppState()
	we := s.CreateOrResetWaitingEpisode(true, "assistant text", true)

	if we == nil {
		t.Fatalf("expected waiting episode")
	}
	if !we.NeedsInterruptedPrefix() {
		t.Fatalf("expected needs interrupted prefix")
	}
	if we.InterruptedAssistantText() != "assistant text" {
		t.Fatalf("unexpected assistant text")
	}

	s.AddSpeechSegment("user says")
	s.MarkTTSDone()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	completed := s.WaitForWaitingEpisode(ctx)
	if completed == nil {
		t.Fatalf("expected completed episode")
	}
	if len(completed.Segments()) != 1 || completed.Segments()[0] != "user says" {
		t.Fatalf("unexpected segments: %v", completed.Segments())
	}
}

func TestWaitingEpisodeCancel(t *testing.T) {
	s := NewAppState()
	s.CreateOrResetWaitingEpisode(false, "", false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if we := s.WaitForWaitingEpisode(ctx); we != nil {
		t.Fatalf("expected nil on cancelled context")
	}
}

func TestPendingToolResults(t *testing.T) {
	s := NewAppState()
	s.MarkToolCallDone([]ToolResult{{Name: "a", Result: "ok"}})

	pending := s.GetPendingToolResults()
	if len(pending) != 1 || pending[0].Name != "a" {
		t.Fatalf("unexpected pending results: %v", pending)
	}
	if len(s.PeekPendingToolResults()) != 0 {
		t.Fatalf("expected pending results cleared after get")
	}
}

func TestCurrentTurnState(t *testing.T) {
	s := NewAppState()
	if s.IsCurrentTurnActive() {
		t.Fatalf("expected no active turn")
	}
	s.SetCurrentTurnActive(true)
	s.SetCurrentTurnEnteredToolCallPhase(true)
	if !s.IsCurrentTurnEnteredToolCallPhase() {
		t.Fatalf("expected entered tool call phase")
	}
	s.SetCurrentTurnActive(false)
	if s.IsCurrentTurnEnteredToolCallPhase() {
		t.Fatalf("expected tool call phase reset")
	}
}
