package toolcalling

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

// SimpleCompletionOptions 无工具调用的 chat/completions 可选参数（温度、max_tokens、JSON 模式）。
type SimpleCompletionOptions struct {
	Temperature        *float64
	MaxTokens          *int
	ResponseFormatJSON bool
}

// ChatCompletionSimple 执行单次非流式补全，不注册 tools；与 Agent.ChatText 相比支持 JSON 模式与显式 Opt 字段。
// 供 PPTAgent_go 等侧车复用，与 [LLMConfig] 使用同一套 BaseURL / APIKey。
func ChatCompletionSimple(
	ctx context.Context,
	config LLMConfig,
	messages []openai.ChatCompletionMessageParamUnion,
	opts *SimpleCompletionOptions,
) (string, error) {
	client := NewOpenAIClient(config)

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(config.Model),
		Messages: messages,
	}
	if opts != nil {
		if opts.Temperature != nil {
			params.Temperature = param.NewOpt(*opts.Temperature)
		}
		if opts.MaxTokens != nil {
			params.MaxTokens = param.NewOpt(int64(*opts.MaxTokens))
		}
		if opts.ResponseFormatJSON {
			params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
			}
		}
	}
	if config.ExtraBody != nil {
		params.SetExtraFields(config.ExtraBody)
	}

	resp, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty choices")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}
