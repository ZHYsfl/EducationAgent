package toolllm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"toolcalling"

	"educationagent/ppt_agent_service_go/internal/config"
	"educationagent/ppt_agent_service_go/internal/deckgen"
)

type Client struct {
	cfg         toolcalling.LLMConfig
	langModel   string
	visionModel string
}

type Options struct {
	JSONMode       bool
	Temperature    *float64
	MaxTokens      *int
	ImageURLs      []string
	ImageDetail    string
	ModelOverride  string
	UseVisionModel bool
}

func New(cfg config.Config) (*Client, error) {
	m := strings.TrimSpace(cfg.PPTAGENTModel)
	base := strings.TrimSpace(cfg.PPTAGENTAPIBase)
	key := strings.TrimSpace(cfg.PPTAGENTAPIKey)
	if m == "" || base == "" || key == "" {
		return nil, fmt.Errorf("请配置 PPTAGENT_MODEL、PPTAGENT_API_BASE、PPTAGENT_API_KEY")
	}
	vm := strings.TrimSpace(cfg.PPTAGENTVisionModel)
	if vm == "" {
		vm = m
	}
	return &Client{
		cfg: toolcalling.LLMConfig{
			APIKey:  key,
			BaseURL: base,
			Model:   m,
		},
		langModel:   m,
		visionModel: vm,
	}, nil
}

func (c *Client) Complete(ctx context.Context, user, system string, opts *Options) (string, error) {
	if c == nil {
		return "", fmt.Errorf("llm nil")
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
	userText := user
	if strings.TrimSpace(userText) == "" && len(imageURLs) > 0 {
		userText = "请结合附图完成下列任务。"
	}
	var userMsg openai.ChatCompletionMessageParamUnion
	if len(imageURLs) > 0 {
		parts := []openai.ChatCompletionContentPartUnionParam{openai.TextContentPart(userText)}
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
		cfg.Model = c.langModel
	}
	simple := &toolcalling.SimpleCompletionOptions{
		Temperature:        temp,
		MaxTokens:          maxTok,
		ResponseFormatJSON: jsonMode,
	}
	out, err := toolcalling.ChatCompletionSimple(ctx, cfg, msgs, simple)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// MergeDecisionByTool 强制使用 tool_calling_go 的 Tool 触发冲突裁决输出。
// 模型必须调用 submit_merge_decision 工具提交结构化结果。
func (c *Client) MergeDecisionByTool(ctx context.Context, user, system string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("llm nil")
	}
	agent := toolcalling.NewAgent(c.cfg, toolcalling.WithMaxToolRetries(2))
	var captured map[string]any
	agent.AddTool(toolcalling.Tool{
		Name:        "submit_merge_decision",
		Description: "提交三路合并裁决结果。冲突场景必须调用。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"merge_status": map[string]any{
					"type": "string",
					"enum": []string{"auto_resolved", "ask_human"},
				},
				"merged_pycode": map[string]any{"type": "string"},
				"question_for_user": map[string]any{"type": "string"},
			},
			"required": []string{"merge_status", "merged_pycode", "question_for_user"},
			"additionalProperties": false,
		},
		Function: func(_ context.Context, args map[string]any) (string, error) {
			captured = args
			b, _ := json.Marshal(args)
			return string(b), nil
		},
	})
	msgs := make([]openai.ChatCompletionMessageParamUnion, 0, 2)
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, openai.SystemMessage(system+"\n你必须调用 submit_merge_decision 工具提交结果。"))
	}
	msgs = append(msgs, openai.UserMessage(user))
	if _, err := agent.Chat(ctx, msgs); err != nil {
		return "", err
	}
	if captured == nil {
		return "", fmt.Errorf("model did not call submit_merge_decision tool")
	}
	b, err := json.Marshal(captured)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// DeckCompleter 实现 deckgen.Completer。
type DeckCompleter struct{ C *Client }

func (d DeckCompleter) Complete(ctx context.Context, user, system string, opts *deckgen.LLMOpts) (string, error) {
	if d.C == nil {
		return "", fmt.Errorf("client nil")
	}
	o := &Options{}
	if opts != nil {
		o.JSONMode = opts.JSONMode
		o.Temperature = opts.Temperature
		o.MaxTokens = opts.MaxTokens
	}
	return d.C.Complete(ctx, user, system, o)
}
