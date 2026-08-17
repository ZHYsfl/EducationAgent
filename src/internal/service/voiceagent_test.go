package service

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent_runtime"
	"educationagent/internal/model"
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

// concurrentVoiceSvc is a test double that sleeps during calls so we can
// verify that multiple tool calls run in parallel.
type concurrentVoiceSvc struct {
	delay         time.Duration
	current       int32
	maxConcurrent int32
}

func (m *concurrentVoiceSvc) enter() {
	c := atomic.AddInt32(&m.current, 1)
	for {
		max := atomic.LoadInt32(&m.maxConcurrent)
		if c <= max || atomic.CompareAndSwapInt32(&m.maxConcurrent, max, c) {
			break
		}
	}
}

func (m *concurrentVoiceSvc) leave() { atomic.AddInt32(&m.current, -1) }

func (m *concurrentVoiceSvc) UpdateRequirements(req map[string]any) (*model.UpdateRequirementsData, error) {
	m.enter()
	time.Sleep(m.delay)
	m.leave()
	return &model.UpdateRequirementsData{MissingFields: []string{}}, nil
}

func (m *concurrentVoiceSvc) RequireConfirm(req model.Requirements) error { return nil }

func (m *concurrentVoiceSvc) SendToPPTAgent(data string) error {
	m.enter()
	time.Sleep(m.delay)
	m.leave()
	return nil
}

func (m *concurrentVoiceSvc) GetMessagesFromPPTAgent() (string, error) { return "", nil }

func TestExecuteToolCallsParallel(t *testing.T) {
	svc := &concurrentVoiceSvc{delay: 100 * time.Millisecond}
	exec := voiceagent.NewExecutor(svc)
	vas := NewVoiceAgentService(agent_runtime.LLMConfig{}, exec)

	out := make(chan model.SSEChunk, 10)
	calls := []voiceagent.ToolCall{
		{Name: "update_requirements", Arguments: map[string]any{"topic": "A"}},
		{Name: "send_to_ppt_agent", Arguments: map[string]any{"data": "B"}},
	}

	start := time.Now()
	results, err := vas.executeToolCalls(context.Background(), calls, out)
	elapsed := time.Since(start)
	close(out)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "update_requirements" || results[1].Name != "send_to_ppt_agent" {
		t.Fatalf("unexpected result order: %v", results)
	}
	if elapsed > 150*time.Millisecond {
		t.Fatalf("tool calls appear sequential, elapsed=%v", elapsed)
	}
	if atomic.LoadInt32(&svc.maxConcurrent) < 2 {
		t.Fatalf("expected concurrent execution, maxConcurrent=%d", svc.maxConcurrent)
	}

	var toolCalls, toolResponses int
	for chunk := range out {
		if chunk.Type == "tool_call" {
			toolCalls++
		}
		if chunk.Type == "tool_response" {
			toolResponses++
		}
	}
	if toolCalls != 2 || toolResponses != 2 {
		t.Fatalf("expected 2 tool_call and 2 tool_response, got %d/%d", toolCalls, toolResponses)
	}
}
