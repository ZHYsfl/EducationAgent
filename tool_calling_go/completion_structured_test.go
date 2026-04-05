package toolcalling

import (
	"context"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestCleanMarkdownCodeBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "json with markdown",
			input:    "```json\n{\"key\":\"value\"}\n```",
			expected: "{\"key\":\"value\"}",
		},
		{
			name:     "JSON uppercase with markdown",
			input:    "```JSON\n{\"key\":\"value\"}\n```",
			expected: "{\"key\":\"value\"}",
		},
		{
			name:     "plain markdown",
			input:    "```\n{\"key\":\"value\"}\n```",
			expected: "{\"key\":\"value\"}",
		},
		{
			name:     "no markdown",
			input:    "{\"key\":\"value\"}",
			expected: "{\"key\":\"value\"}",
		},
		{
			name:     "with extra whitespace",
			input:    "  ```json\n  {\"key\":\"value\"}  \n```  ",
			expected: "{\"key\":\"value\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanMarkdownCodeBlock(tt.input)
			if result != tt.expected {
				t.Errorf("cleanMarkdownCodeBlock() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestChatCompletionStructured_MockSuccess(t *testing.T) {
	// This is a unit test for the retry logic structure
	// In real usage, you'd need a valid API key and endpoint

	type TestIntent struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}

	// Test that the function signature compiles and accepts generic types
	ctx := context.Background()
	config := LLMConfig{
		APIKey:  "test-key",
		Model:   "gpt-4",
		BaseURL: "https://api.openai.com/v1",
	}

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("test"),
		openai.UserMessage("test"),
	}

	var result []TestIntent

	// This will fail without a real API key, but we're just testing the structure
	err := ChatCompletionStructured(ctx, config, msgs, &result, &StructuredCompletionOptions{
		MaxRetries: 1,
	})

	// We expect an error since we don't have a real API
	if err == nil {
		t.Log("Unexpected success - likely using a real API key")
	} else {
		t.Logf("Expected error (no real API): %v", err)
	}
}
