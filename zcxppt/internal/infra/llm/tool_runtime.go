package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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

	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()

	refContext := r.buildReferenceContext(req)

	sys := openai.SystemMessage(`你是一个PPT反馈合并编排器。请对每一条用户反馈指令进行语义理解，处理歧义和冲突，最终必须调用 emit_merge_result 一次完成合并。不要输出自由文本。

指令格式说明：
- modify: 修改指定页面内容（target_page_id 指定页面）
- insert_before/insert_after: 在目标页前/后插入新页
- delete: 删除指定页面
- global_modify: 全局修改所有页面
- reorder: 页面重排

当用户引用了参考文件（reference_files）时：
1) 先理解参考文件中与本条反馈意图相关的内容片段
2) 将参考内容与当前页面代码进行融合，确保参考内容被正确应用到页面中
3) 如果参考文件的样式指南（style_guide）存在，请在修改时遵循该样式
4) 特别重要：当参考内容与当前页面内容存在冲突时，以参考文件的内容为准

请结合以下参考上下文，对当前页面代码进行融合修改：` + "\n\n" + refContext)

	user := openai.UserMessage(fmt.Sprintf("task_id=%s page_id=%s current_code=%s raw_text=%s intents=%v",
		req.TaskID, req.ViewingPageID, current.PyCode, req.RawText, req.Intents))

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

// buildReferenceContext serializes the reference file context for injection into the LLM prompt.
// If RefFusionResult is already pre-computed, use it directly; otherwise, build a summary from ReferenceFiles.
func (r *ToolCallingRuntime) buildReferenceContext(req model.FeedbackRequest) string {
	if len(req.ReferenceFiles) == 0 && req.RefFusionResult == nil {
		return "[无参考资料]"
	}

	var parts []string

	if req.RefFusionResult != nil {
		rf := req.RefFusionResult
		if rf.ExtractedText != "" {
			parts = append(parts, "[参考内容摘要]\n"+rf.ExtractedText)
		}
		if rf.StyleGuide != "" {
			parts = append(parts, "[样式指南]\n"+rf.StyleGuide)
		}
		if len(rf.TopicHints) > 0 {
			parts = append(parts, "[知识补充]\n"+strings.Join(rf.TopicHints, "\n"))
		}
	}

	if len(req.ReferenceFiles) > 0 && req.RefFusionResult == nil {
		// Fallback: list reference files without pre-computed fusion
		parts = append(parts, "[参考文件]")
		for _, f := range req.ReferenceFiles {
			ft := strings.ToLower(strings.TrimSpace(f.FileType))
			var typeLabel string
			switch ft {
			case "pdf":
				typeLabel = "PDF文档"
			case "docx":
				typeLabel = "Word文档"
			case "pptx":
				typeLabel = "PPT演示文稿"
			case "image", "img", "png", "jpg", "jpeg":
				typeLabel = "图片"
			case "video", "mp4":
				typeLabel = "视频"
			default:
				typeLabel = ft
			}
			part := fmt.Sprintf("- 文件ID=%s 类型=%s", f.FileID, typeLabel)
			if f.Instruction != "" {
				part += fmt.Sprintf(" 抽取指令=%s", f.Instruction)
			}
			parts = append(parts, part)
		}
		parts = append(parts, "（如需使用参考内容，请结合上述文件信息与当前页面代码进行融合）")
	}

	return strings.Join(parts, "\n")
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
