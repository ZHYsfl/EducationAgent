package toolcalling

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// ChatCompletionForwardJSON 将 OpenAI 兼容的 POST /v1/chat/completions 请求 JSON
// 反序列化为 ChatCompletionNewParams，经 openai-go 转发上游，并返回响应 JSON（非流式）。
// defaultModel 在请求体未带 model 时填入（如来自 PPTAGENT_MODEL）。
func ChatCompletionForwardJSON(ctx context.Context, config LLMConfig, requestJSON []byte, defaultModel string) ([]byte, error) {
	var streamProbe struct {
		Stream *bool `json:"stream"`
	}
	_ = json.Unmarshal(requestJSON, &streamProbe)
	if streamProbe.Stream != nil && *streamProbe.Stream {
		return nil, fmt.Errorf("PPTAgent_go: 请使用 stream=false，流式尚未实现")
	}

	var params openai.ChatCompletionNewParams
	if err := json.Unmarshal(requestJSON, &params); err != nil {
		return nil, fmt.Errorf("invalid chat completion json: %w", err)
	}
	if params.Model == "" && defaultModel != "" {
		params.Model = shared.ChatModel(defaultModel)
	}
	if config.ExtraBody != nil {
		params.SetExtraFields(config.ExtraBody)
	}

	client := NewOpenAIClient(config)
	resp, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(resp)
}
