package agent_runtime

import (
	"context"
	"testing"
)

func TestToolFunc(t *testing.T) {
	getWeather := func(ctx context.Context, city string) (string, error) {
		return "snowy", nil
	}

	tool := Tool{
		Name:        "TestTool",
		Description: "A test tool",
		Func:        getWeather,
		Parameters:  map[string]any{
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

	result, err := tool.Func(context.Background(), "New York")
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
        message : openai.ToolMessage("call_xxx", "This is a test response"),
		content: "This is a test response",              
		status:  "success",    
	} 
	
	if toolResp.status != "success" {
		t.Fatalf("Expected status 'success', but got %q", toolResp.status)
	}
	if toolResp.content != "This is a test response" {
		t.Fatalf("Expected content 'This is a test response', but got %q", toolResp.content)
	}
	if toolResp.message.GetContent() != "This is a test response" {
		t.Fatalf("Expected message content 'This is a test response', but got %q", toolResp.message.GetContent())
	}
	if toolResp.message.GetRole() != "call_xxx" {
		t.Fatalf("Expected message role 'call_xxx', but got %q", toolResp.message.GetRole())
	}
	if toolResp.errorType != nil {
		t.Fatalf("Expected errorType to be nil, but got %v", *toolResp.errorType)
	}
}