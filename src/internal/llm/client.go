// Package llm wraps the DeepSeek streaming chat API behind a small event channel.
package llm

import (
	"context"

	"agent_runtime"
	"github.com/openai/openai-go/v3"
)

// Config is everything needed to reach the model service.
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
}

type Client struct {
	client openai.Client
	model  string
}

func NewClient(cfg Config) *Client {
	rtCfg := &agent_runtime.LLMConfig{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
	}
	return &Client{
		client: agent_runtime.NewOpenAIClient(rtCfg),
		model:  cfg.Model,
	}
}

// Stream starts a streaming completion and returns the event channel.
// Cancelling ctx closes the underlying HTTP connection and the channel;
// the consumer can simply stop reading.
func (c *Client) Stream(
	ctx context.Context,
	messages []openai.ChatCompletionMessageParamUnion,
	tools []openai.ChatCompletionToolUnionParam,
) <-chan Event {
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(c.model),
		Messages: messages,
		Tools:    tools,
	}
	// DeepSeek rejects chat requests unless non-reasoning mode is explicit.
	params.SetExtraFields(map[string]any{
		"thinking": map[string]any{"type": "disabled"},
	})

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	return eventsFromStream(ctx, stream)
}
