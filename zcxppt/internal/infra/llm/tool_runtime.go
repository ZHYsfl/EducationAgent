package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go/v3"
	toolcalling "tool_calling_go"

	"zcxppt/internal/model"
)

type ToolRuntime interface {
	RunFeedbackLoop(ctx context.Context, req model.FeedbackRequest, current model.PageRenderResponse) (model.MergeResult, error)
}

type RuntimeConfig struct {
	Mode    string
	APIKey  string
	Model   string
	BaseURL string
}

type chatFunc func(context.Context, []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error)
type extractFunc func([]openai.ChatCompletionMessageParamUnion) (model.MergeResult, bool)

func NewToolRuntime(cfg RuntimeConfig) ToolRuntime {
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), "real") {
		return NewToolCallingRuntime(cfg)
	}
	return &MockToolRuntime{}
}

type ToolCallingRuntime struct {
	agent     *toolcalling.Agent
	chat      chatFunc
	extractor extractFunc
}

func NewToolCallingRuntime(cfg RuntimeConfig) *ToolCallingRuntime {
	a := toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
		BaseURL: cfg.BaseURL,
	})
	r := &ToolCallingRuntime{agent: a, extractor: extractMergeResultFromMessages}
	r.chat = a.Chat
	r.registerTools()
	return r
}

// NewToolCallingRuntimeWithHooks is used by tests to inject fake chat/extractor.
func NewToolCallingRuntimeWithHooks(chat chatFunc, extractor extractFunc) *ToolCallingRuntime {
	if extractor == nil {
		extractor = extractMergeResultFromMessages
	}
	return &ToolCallingRuntime{chat: chat, extractor: extractor}
}

func (r *ToolCallingRuntime) registerTools() {
	if r.agent == nil {
		return
	}
	r.agent.AddTool(toolcalling.Tool{
		Name:        "emit_merge_result",
		Description: "Emit final merge result JSON for one feedback. Must be called exactly once.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"merge_status": map[string]any{
					"type": "string",
					"enum": []string{"auto_resolved", "ask_human"},
				},
				"merged_pycode":     map[string]any{"type": "string"},
				"question_for_user": map[string]any{"type": "string"},
			},
			"required": []string{"merge_status"},
		},
		Function: func(ctx context.Context, args map[string]any) (string, error) {
			_ = ctx
			status := fmt.Sprintf("%v", args["merge_status"])
			res := model.MergeResult{MergeStatus: status}
			if v, ok := args["merged_pycode"]; ok {
				res.MergedPyCode = fmt.Sprintf("%v", v)
			}
			if v, ok := args["question_for_user"]; ok {
				res.QuestionForUser = fmt.Sprintf("%v", v)
			}
			b, _ := json.Marshal(res)
			return string(b), nil
		},
	})
}

func (r *ToolCallingRuntime) RunFeedbackLoop(ctx context.Context, req model.FeedbackRequest, current model.PageRenderResponse) (model.MergeResult, error) {
	if r.chat == nil {
		return model.MergeResult{}, errors.New("tool runtime chat function is not configured")
	}
	sys := openai.SystemMessage("You are a PPT merge orchestrator. For every request, reason over ambiguous natural language intents and conflicts. You MUST finalize by calling emit_merge_result exactly once. Do not return free text.")
	user := openai.UserMessage(fmt.Sprintf("task_id=%s page_id=%s current_code=%s raw_text=%s intents=%v", req.TaskID, req.ViewingPageID, current.PyCode, req.RawText, req.Intents))

	msgs, err := r.chat(ctx, []openai.ChatCompletionMessageParamUnion{sys, user})
	if err != nil {
		return model.MergeResult{}, err
	}

	extractor := r.extractor
	if extractor == nil {
		extractor = extractMergeResultFromMessages
	}
	result, ok := extractor(msgs)
	if !ok {
		return model.MergeResult{}, errors.New("tool runtime did not emit valid merge result")
	}
	if result.MergeStatus != "auto_resolved" && result.MergeStatus != "ask_human" {
		return model.MergeResult{}, errors.New("invalid merge_status from tool runtime")
	}
	if result.MergeStatus == "auto_resolved" && strings.TrimSpace(result.MergedPyCode) == "" {
		return model.MergeResult{}, errors.New("merged_pycode is required when merge_status=auto_resolved")
	}
	if result.MergeStatus == "ask_human" && strings.TrimSpace(result.QuestionForUser) == "" {
		return model.MergeResult{}, errors.New("question_for_user is required when merge_status=ask_human")
	}
	return result, nil
}

func extractMergeResultFromMessages(msgs []openai.ChatCompletionMessageParamUnion) (model.MergeResult, bool) {
	for i := len(msgs) - 1; i >= 0; i-- {
		b, err := json.Marshal(msgs[i])
		if err != nil {
			continue
		}
		var raw any
		if err := json.Unmarshal(b, &raw); err != nil {
			continue
		}
		for _, s := range collectStrings(raw) {
			var out model.MergeResult
			if strings.Contains(s, "merge_status") && json.Unmarshal([]byte(s), &out) == nil && out.MergeStatus != "" {
				return out, true
			}
		}
	}
	return model.MergeResult{}, false
}

func collectStrings(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case map[string]any:
		out := make([]string, 0)
		for _, vv := range x {
			out = append(out, collectStrings(vv)...)
		}
		return out
	case []any:
		out := make([]string, 0)
		for _, vv := range x {
			out = append(out, collectStrings(vv)...)
		}
		return out
	default:
		return nil
	}
}

type MockToolRuntime struct{}

func NewMockToolRuntime() *MockToolRuntime { return &MockToolRuntime{} }

func (m *MockToolRuntime) RunFeedbackLoop(ctx context.Context, req model.FeedbackRequest, current model.PageRenderResponse) (model.MergeResult, error) {
	_ = ctx
	_ = req
	return model.MergeResult{
		MergeStatus:  "auto_resolved",
		MergedPyCode: current.PyCode + "\n# merged by mock runtime",
	}, nil
}
