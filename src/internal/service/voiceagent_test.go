package service

import (
	"strings"
	"testing"

	"educationagent/internal/state"
	"educationagent/internal/voiceagent"

	"github.com/openai/openai-go/v3"
)

func TestAssembleUserMessagesNormal(t *testing.T) {
	s := state.NewAppState()
	msgs := assembleUserMessages(s, []string{"hello"}, false, nil)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (saying + status), got %d", len(msgs))
	}
	if !containsContent(msgs[0], "hello") {
		t.Fatalf("expected saying words")
	}
	if !containsContent(msgs[1], "<queue_status>empty</queue_status>") {
		t.Fatalf("expected status bar")
	}
}

func TestAssembleUserMessagesInterrupted(t *testing.T) {
	s := state.NewAppState()
	msgs := assembleUserMessages(s, []string{"make it concise"}, true, nil)
	if !containsContent(msgs[0], "</interrupted>make it concise") {
		t.Fatalf("expected interrupted prefix, got %v", msgs)
	}
}

func TestAssembleUserMessagesWithPendingResults(t *testing.T) {
	s := state.NewAppState()
	pending := []state.ToolResult{{Name: "update_requirements", Result: "all fields are updated"}}
	msgs := assembleUserMessages(s, []string{"ok"}, false, pending)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if !containsContent(msgs[0], "<tool_response>") {
		t.Fatalf("expected tool response message first")
	}
}

func TestRenderUserMessages(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello"),
		openai.UserMessage("world"),
	}
	rendered := renderUserMessages(msgs)
	if !contains(rendered, "<|im_start|>user") {
		t.Fatalf("expected im_start markers")
	}
	if !contains(rendered, "hello") || !contains(rendered, "world") {
		t.Fatalf("expected content")
	}
}

func TestBuildAssistantContent(t *testing.T) {
	calls := []voiceagent.ToolCall{
		{Name: "update_requirements", Arguments: map[string]any{"topic": "math"}},
	}
	content := buildAssistantContent("ok ", calls)
	if !contains(content, "ok ") {
		t.Fatalf("expected spoken text")
	}
	if !contains(content, "<tool_call>") {
		t.Fatalf("expected tool call tag")
	}
}

func containsContent(msg openai.ChatCompletionMessageParamUnion, substr string) bool {
	if msg.OfUser == nil {
		return false
	}
	var content string
	if msg.OfUser.Content.OfString.Valid() {
		content = msg.OfUser.Content.OfString.Value
	} else {
		for _, p := range msg.OfUser.Content.OfArrayOfContentParts {
			if p.OfText != nil {
				content += p.OfText.Text
			}
		}
	}
	return contains(content, substr)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
