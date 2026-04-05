package toolcalling

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
)

// StructuredCompletionOptions 结构化输出的可选参数。
type StructuredCompletionOptions struct {
	Temperature *float64
	MaxTokens   *int
	MaxRetries  int // 默认 3 次
}

// ChatCompletionStructured 执行带重试机制的结构化 JSON 输出。
// 自动启用 JSON 模式，清理 markdown 代码块，解析为目标类型 T。
// 如果 JSON 解析失败，会重试最多 MaxRetries 次（默认 3 次）。
//
// 用法示例：
//
//	type Intent struct {
//	    Action string `json:"action"`
//	    Target string `json:"target"`
//	}
//	var result []Intent
//	err := ChatCompletionStructured(ctx, config, messages, &result, nil)
func ChatCompletionStructured[T any](
	ctx context.Context,
	config LLMConfig,
	messages []openai.ChatCompletionMessageParamUnion,
	result *T,
	opts *StructuredCompletionOptions,
) error {
	maxRetries := 3
	if opts != nil && opts.MaxRetries > 0 {
		maxRetries = opts.MaxRetries
	}

	simpleOpts := &SimpleCompletionOptions{
		ResponseFormatJSON: true,
	}
	if opts != nil {
		simpleOpts.Temperature = opts.Temperature
		simpleOpts.MaxTokens = opts.MaxTokens
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		resp, err := ChatCompletionSimple(ctx, config, messages, simpleOpts)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: completion failed: %w", attempt+1, err)
			continue
		}

		// 清理 markdown 代码块
		cleaned := cleanMarkdownCodeBlock(resp)

		// 尝试解析 JSON
		if err := json.Unmarshal([]byte(cleaned), result); err != nil {
			lastErr = fmt.Errorf("attempt %d: json parse failed: %w (raw: %q)", attempt+1, err, cleaned)
			continue
		}

		// 成功
		return nil
	}

	return fmt.Errorf("structured completion failed after %d attempts: %w", maxRetries, lastErr)
}

// cleanMarkdownCodeBlock 清理 LLM 输出中的 markdown 代码块标记。
// 支持 ```json、```、``` 等常见格式。
func cleanMarkdownCodeBlock(s string) string {
	s = strings.TrimSpace(s)

	// 移除开头的代码块标记
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")

	// 移除结尾的代码块标记
	s = strings.TrimSuffix(s, "```")

	return strings.TrimSpace(s)
}
