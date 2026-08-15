package agent_runtime

import (
	"context"
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestNewAgent(t *testing.T) {
	config := &LLMConfig{
		APIKey:  "test-api-key",
		BaseURL: "https://test.example.com",
		Model:   "test-model",
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

	tools := []*Tool{
		&tool,
	}

	agent := NewAgent(config, tools)
	if agent == nil {
		t.Fatal("Expected non-nil agent")
	}
	if agent.config != config {
		t.Error("Expected agent config to match input config")
	}
	if len(agent.tools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(agent.tools))
	}
	if agent.debug != false {
		t.Errorf("Expected debug to be false by default, got %v", agent.debug)
	}
	if agent.maxToolRetries != 3 {
		t.Errorf("Expected maxToolRetries to be 3 by default, got %d", agent.maxToolRetries)
	}
}

func TestNewAgentWithOptions(t *testing.T) {
	config := &LLMConfig{
		APIKey:  "test-api-key",
		BaseURL: "https://test.example.com",
		Model:   "test-model",
	}

	agent := NewAgent(
		config,
		nil,
		WithDebug(true),
		WithMaxToolRetries(5),
	)

	if agent.debug != true {
		t.Errorf("Expected debug to be true, got %v", agent.debug)
	}
	if agent.maxToolRetries != 5 {
		t.Errorf("Expected maxToolRetries to be 5, got %d", agent.maxToolRetries)
	}
}

func TestAddTool(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, nil)

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

	agent.AddTool(&tool)

	if len(agent.tools) != 1 {
		t.Errorf("Expected 1 tool after AddTool, got %d", len(agent.tools))
	}
	if agent.tools[0].Name != "TestTool" {
		t.Errorf("Expected tool name 'TestTool', got %q", agent.tools[0].Name)
	}
}

func TestRemoveTool(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, []*Tool{
		{Name: "keep_me"},
		{Name: "remove_me"},
	})

	agent.RemoveTool("remove_me")

	if len(agent.tools) != 1 {
		t.Errorf("Expected 1 tool after RemoveTool, got %d", len(agent.tools))
	}
	if agent.tools[0].Name != "keep_me" {
		t.Errorf("Expected remaining tool name 'keep_me', got %q", agent.tools[0].Name)
	}

	agent.RemoveTool("not_exists")
	if len(agent.tools) != 1 {
		t.Errorf("Expected still 1 tool after removing non-existent tool, got %d", len(agent.tools))
	}
}

func TestGetTools(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, []*Tool{
		{
			Name:        "get_weather",
			Description: "Get the weather",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type": "string",
					},
				},
			},
		},
	})

	tools := agent.GetTools()
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool param, got %d", len(tools))
	}

	funcTool := tools[0].OfChatCompletionFunctionTool
	if funcTool == nil {
		t.Fatal("Expected function tool, got nil")
	}

	if funcTool.Function.Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got %q", funcTool.Function.Name)
	}

	desc := funcTool.Function.Description.Or("")
	if desc != "Get the weather" {
		t.Errorf("Expected tool description 'Get the weather', got %q", desc)
	}

	if funcTool.Function.Parameters == nil {
		t.Error("Expected non-nil tool parameters")
	}
}
