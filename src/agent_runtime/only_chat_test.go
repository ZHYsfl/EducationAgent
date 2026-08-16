package agent_runtime

import (
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestBuildChatCompletionParamsWithoutTool(t *testing.T) {
	agent := NewAgent(&LLMConfig{
		Model: "test-model",
	}, nil)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello"),
	}

	params := agent.BuildChatCompletionParams(messages, false)

	if params.Model != openai.ChatModel("test-model") {
		t.Errorf("Expected model 'test-model', got %q", params.Model)
	}
	if len(params.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(params.Messages))
	}
	if params.Tools != nil {
		t.Error("Expected no tools when withTool is false")
	}
	if params.ToolChoice.OfAuto.Valid() {
		t.Error("Expected no tool choice when withTool is false")
	}
}

func TestBuildChatCompletionParamsWithTool(t *testing.T) {
	agent := NewAgent(&LLMConfig{
		Model: "test-model",
	}, []*Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters: map[string]any{
				"type": "object",
			},
		},
	})

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("what's the weather"),
	}

	params := agent.BuildChatCompletionParams(messages, true)

	if len(params.Tools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(params.Tools))
	}
	if !params.ToolChoice.OfAuto.Valid() {
		t.Error("Expected tool choice 'auto' when withTool is true")
	}
}

func TestBuildChatCompletionParamsWithConfig(t *testing.T) {
	agent := NewAgent(&LLMConfig{
		Model:            "test-model",
		Temperature:      float64Ptr(0.7),
		MaxTokens:        intPtr(100),
		TopP:             float64Ptr(0.9),
		PresencePenalty:  float64Ptr(0.5),
		FrequencyPenalty: float64Ptr(0.5),
		ExtraBody: map[string]any{
			"custom_key": "custom_value",
		},
	}, nil)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello"),
	}

	params := agent.BuildChatCompletionParams(messages, false)

	if !params.Temperature.Valid() || params.Temperature.Value != 0.7 {
		t.Errorf("Expected temperature 0.7, got %v", params.Temperature)
	}
	if !params.MaxTokens.Valid() || params.MaxTokens.Value != 100 {
		t.Errorf("Expected max tokens 100, got %v", params.MaxTokens)
	}
	if !params.TopP.Valid() || params.TopP.Value != 0.9 {
		t.Errorf("Expected top_p 0.9, got %v", params.TopP)
	}
	if !params.PresencePenalty.Valid() || params.PresencePenalty.Value != 0.5 {
		t.Errorf("Expected presence penalty 0.5, got %v", params.PresencePenalty)
	}
	if !params.FrequencyPenalty.Valid() || params.FrequencyPenalty.Value != 0.5 {
		t.Errorf("Expected frequency penalty 0.5, got %v", params.FrequencyPenalty)
	}
	extras := params.ExtraFields()
	if extras == nil {
		t.Fatal("Expected extra fields to be set")
	}
	if extras["custom_key"] != "custom_value" {
		t.Errorf("Expected extra field custom_key='custom_value', got %v", extras["custom_key"])
	}
}

func TestChatWithoutToolCallStreamReturnsChannel(t *testing.T) {
	agent := NewAgent(&LLMConfig{
		Model: "test-model",
	}, nil)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello"),
	}

	ch := agent.ChatWithoutToolCallStream(nil, messages)
	if ch == nil {
		t.Fatal("Expected non-nil channel")
	}
	// Drain the channel to avoid leaking the goroutine on a closed context.
	go func() {
		for range ch {
		}
	}()
}
