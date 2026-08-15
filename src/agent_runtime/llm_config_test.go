package agent_runtime

import (
	"context"
	"testing"
)

func TestLLMConfig(t *testing.T) {
	config := LLMConfig{
		APIKey: "test_api_key",
		BaseURL: "https://api.example.com",
		Model: "test_model",
		Temperature: float64Ptr(0.7),
		MaxTokens: intPtr(100),
		TopP: float64Ptr(0.9),
		TopK: intPtr(50),
		Timeout: durationPtr(5 * time.Second),
		PresencePenalty: float64Ptr(0.5),
		FrequencyPenalty: float64Ptr(0.5),
		ExtraBody: map[string]any{
			"custom_param": "custom_value",
		},
	}

	if config.APIKey != "test_api_key" {
		t.Errorf("Expected APIKey to be 'test_api_key', got '%s'", config.APIKey)
	}
	if config.BaseURL != "https://api.example.com" {
		t.Errorf("Expected BaseURL to be 'https://api.example.com', got '%s'", config.BaseURL)
	}
	if config.Model != "test_model" {
		t.Errorf("Expected Model to be 'test_model', got '%s'", config.Model)
	}
	if *config.Temperature != 0.7 {
		t.Errorf("Expected Temperature to be 0.7, got %f", *config.Temperature)
	}
	if *config.MaxTokens != 100 {
		t.Errorf("Expected MaxTokens to be 100, got %d", *config.MaxTokens)
	}
	if *config.TopP != 0.9 {
		t.Errorf("Expected TopP to be 0.9, got %f", *config.TopP)
	}		
	if *config.TopK != 50 {
		t.Errorf("Expected TopK to be 50, got %d", *config.TopK)
	}
	if *config.Timeout != 5*time.Second {
		t.Errorf("Expected Timeout to be 5s, got %s", config.Timeout.String())
	}
	if *config.PresencePenalty != 0.5 {
		t.Errorf("Expected PresencePenalty to be 0.5, got %f", *config.PresencePenalty)
	}
	if *config.FrequencyPenalty != 0.5 {
		t.Errorf("Expected FrequencyPenalty to be 0.5, got %f", *config.FrequencyPenalty)
	}
	if config.ExtraBody["custom_param"] != "custom_value" {
		t.Errorf("Expected ExtraBody['custom_param'] to be 'custom_value', got '%v'", config.ExtraBody["custom_param"])
	}
}