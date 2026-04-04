// Package infer 提供与 Python AsyncLLM(prompt, system_message=..., return_json=...) 等价的推理 API，
// 底层经 tool_calling_go 的 openai-go 客户端调用 Chat Completions（支持多模态与 JSON 模式）。
package infer

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"
	"toolcalling"
)

// Client 封装环境变量配置；语言模型与可选的视觉模型名可分开配置。
type Client struct {
	cfg           toolcalling.LLMConfig
	languageModel string
	visionModel   string
}

// NewClientFromEnv 使用 PPTAGENT_MODEL、PPTAGENT_VISION_MODEL（可选，默认与 MODEL 相同）、
// PPTAGENT_API_BASE、PPTAGENT_API_KEY。
func NewClientFromEnv() (*Client, error) {
	language := strings.TrimSpace(os.Getenv("PPTAGENT_MODEL"))
	vision := strings.TrimSpace(os.Getenv("PPTAGENT_VISION_MODEL"))
	base := strings.TrimSpace(os.Getenv("PPTAGENT_API_BASE"))
	key := strings.TrimSpace(os.Getenv("PPTAGENT_API_KEY"))
	if language == "" {
		return nil, fmt.Errorf("PPTAGENT_MODEL 未设置")
	}
	if base == "" {
		return nil, fmt.Errorf("PPTAGENT_API_BASE 未设置")
	}
	if key == "" {
		return nil, fmt.Errorf("PPTAGENT_API_KEY 未设置")
	}
	if vision == "" {
		vision = language
	}
	cfg := toolcalling.LLMConfig{
		APIKey:  key,
		BaseURL: base,
		Model:   language,
	}
	return &Client{cfg: cfg, visionModel: vision, languageModel: language}, nil
}

// Options 可选生成参数。
type Options struct {
	Temperature    *float64
	MaxTokens      *int
	JSONMode       bool // response_format json_object
	ImageURLs      []string // http(s) URL 或 data:image/...;base64,...
	ImageDetail    string   // auto | low | high，空则省略
	ModelOverride  string   // 非空时覆盖语言/视觉默认模型
	UseVisionModel bool     // 有图时 true 则使用 PPTAGENT_VISION_MODEL
}

// Complete 补全：无图时为纯文本 user；有图时为 text + image_url 多段 content。
func (c *Client) Complete(ctx context.Context, user string, system string, opts *Options) (string, error) {
	if c == nil {
		return "", fmt.Errorf("infer client nil")
	}

	var jsonMode bool
	var temp *float64
	var maxTok *int
	var imageURLs []string
	var imageDetail string
	var modelOverride string
	var useVision bool
	if opts != nil {
		jsonMode = opts.JSONMode
		temp = opts.Temperature
		maxTok = opts.MaxTokens
		imageURLs = opts.ImageURLs
		imageDetail = opts.ImageDetail
		modelOverride = strings.TrimSpace(opts.ModelOverride)
		useVision = opts.UseVisionModel
	}

	// 仅有图时给默认文本，避免空 user 消息
	userText := user
	if strings.TrimSpace(userText) == "" && len(imageURLs) > 0 {
		userText = "请结合附图完成下列任务。"
	}

	var userMsg openai.ChatCompletionMessageParamUnion
	if len(imageURLs) > 0 {
		parts := []openai.ChatCompletionContentPartUnionParam{
			openai.TextContentPart(userText),
		}
		detail := strings.TrimSpace(imageDetail)
		for _, raw := range imageURLs {
			u := strings.TrimSpace(raw)
			if u == "" {
				continue
			}
			ip := openai.ChatCompletionContentPartImageImageURLParam{URL: u}
			if detail != "" {
				ip.Detail = detail
			}
			parts = append(parts, openai.ImageContentPart(ip))
		}
		if len(parts) == 1 {
			// 过滤后无有效图，退回纯文本
			userMsg = openai.UserMessage(userText)
		} else {
			userMsg = openai.UserMessage(parts)
		}
	} else {
		userMsg = openai.UserMessage(userText)
	}

	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, 2)
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, openai.SystemMessage(system))
	}
	msgs = append(msgs, userMsg)

	cfg := c.cfg
	switch {
	case modelOverride != "":
		cfg.Model = modelOverride
	case len(imageURLs) > 0 && useVision:
		cfg.Model = c.visionModel
	default:
		cfg.Model = c.languageModel
	}

	simpleOpts := &toolcalling.SimpleCompletionOptions{
		Temperature:        temp,
		MaxTokens:          maxTok,
		ResponseFormatJSON: jsonMode,
	}
	out, err := toolcalling.ChatCompletionSimple(ctx, cfg, msgs, simpleOpts)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CompleteSimple 无额外选项，对应常见 await llm(prompt, system_message=...)。
func (c *Client) CompleteSimple(ctx context.Context, prompt, systemMessage string) (string, error) {
	return c.Complete(ctx, prompt, systemMessage, nil)
}

// ToolcallingConfig 返回与当前 Client 一致的 LLMConfig（供 /v1/chat/completions 经 tool_calling_go 转发）。
func (c *Client) ToolcallingConfig() toolcalling.LLMConfig {
	if c == nil {
		return toolcalling.LLMConfig{}
	}
	return c.cfg
}

// DefaultModelName 返回 PPTAGENT_MODEL（请求未指定 model 时的默认值）。
func (c *Client) DefaultModelName() string {
	if c == nil {
		return ""
	}
	return c.languageModel
}
