package agent_runtime

import (
	"context"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestToolFunc(t *testing.T) {
	getWeather := func(ctx context.Context, args map[string]any) (string, error) {
		return "snowy", nil
	}

	tool := Tool{
		Name:        "TestTool",
		Description: "A test tool",
		Func:        getWeather,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{
					"type":        "string",
					"description": "City name",
				},
			},
			"required": []any{"city"},
		},
	}

	result, err := tool.Func(context.Background(), map[string]any{"city": "New York"})
	if err != nil {
		t.Fatalf("Tool function returned an error: %v", err)
	}

	expected := "snowy"
	if result != expected {
		t.Fatalf("Expected result %q, but got %q", expected, result)
	}
}

func TestToolResponse(t *testing.T) {
	toolResp := ToolResponse{
		message: openai.ToolMessage("This is a test response", "call_xxx"),
		content: "This is a test response",
		status:  "success",
	}

	if toolResp.status != "success" {
		t.Fatalf("Expected status 'success', but got %q", toolResp.status)
	}
	if toolResp.content != "This is a test response" {
		t.Fatalf("Expected content 'This is a test response', but got %q", toolResp.content)
	}

	content := toolResp.message.GetContent().AsAny()
	contentPtr, ok := content.(*string)
	if !ok {
		t.Fatalf("Expected message content to be *string, got %T", content)
	}
	if contentPtr == nil {
		t.Fatal("Expected non-nil message content pointer")
	}
	if *contentPtr != "This is a test response" {
		t.Fatalf("Expected message content 'This is a test response', but got %q", *contentPtr)
	}

	role := toolResp.message.GetRole()
	if role == nil {
		t.Fatal("Expected non-nil message role")
	}

	toolCallID := toolResp.message.GetToolCallID()
	if toolCallID == nil || *toolCallID != "call_xxx" {
		expected := "call_xxx"
		got := "<nil>"
		if toolCallID != nil {
			got = *toolCallID
		}
		t.Fatalf("Expected message tool call id %q, but got %q", expected, got)
	}

	if toolResp.errorType != nil {
		t.Fatalf("Expected errorType to be nil, but got %v", *toolResp.errorType)
	}
}
