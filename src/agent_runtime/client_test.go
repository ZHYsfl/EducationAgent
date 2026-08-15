package agent_runtime

import (
	"testing"
)

func TestNewOpenAIClient(t *testing.T) {
	config := &LLMConfig{
		APIKey:  "test-api-key",
		BaseURL: "test-base-url",
	}
	client := NewOpenAIClient(config)
	// openai.Client is a value type, so we verify the options were set.
	if len(client.Options) == 0 {
		t.Errorf("Expected client options to be set")
	}
}
