package agent_runtime

import (
	"context"
	"testing"
)

func TestNewOpenAIClient(t *testing.T) {
	config := &LLMConfig{
		APIKey: "test-api-key",
		BaseURL: "test-base-url",
	}
	client := NewOpenAIClient(config)
	if client == nil {
		t.Errorf("Expected non-nil client")
	}
}