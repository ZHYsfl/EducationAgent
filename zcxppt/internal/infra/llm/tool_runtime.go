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
	RunFeedbackLoop(ctx context.Context, req model.FeedbackRequest, current model.PageRenderResponse, baseCode string) (model.MergeResult, error)
}

type RuntimeConfig struct {
	Mode    string
	APIKey  string
	Model   string
	BaseURL string
}

type chatFunc func(context.Context, []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error)
type extractFunc func([]openai.ChatCompletionMessageParamUnion) (model.MergeResult, bool)

// mergeServiceInterface 是 MergeService 必须实现的接口。
type mergeServiceInterface interface {
	ThreeWayMerge(in model.ThreeWayMergeInput) model.MergeResult
}

func NewToolRuntime(cfg RuntimeConfig) ToolRuntime {
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), "real") {
		return NewToolCallingRuntime(cfg)
	}
	return &MockToolRuntime{}
}

type ToolCallingRuntime struct {
	agent        *toolcalling.Agent
	chat         chatFunc
	extractor    extractFunc
	mergeService mergeServiceInterface
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

// InjectMergeService 注入三路合并服务（由 FeedbackService 在构造时注入）。
func (r *ToolCallingRuntime) InjectMergeService(ms mergeServiceInterface) {
	r.mergeService = ms
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

// RunFeedbackLoop 执行反馈合并循环。
// 它先让 LLM 基于 base（基线快照）和 current（当前页面）生成 merged_pycode（新版本），
// 然后通过三路合并（base/current/incoming）决定最终结果。
func (r *ToolCallingRuntime) RunFeedbackLoop(ctx context.Context, req model.FeedbackRequest, current model.PageRenderResponse, baseCode string) (model.MergeResult, error) {
	if r.chat == nil {
		return model.MergeResult{}, errors.New("tool runtime chat function is not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()

	refContext := r.buildReferenceContext(req)

	// System Prompt：明确告诉 LLM 这是三路合并场景
	sys := openai.SystemMessage(`你是一个PPT反馈合并编排器。请对每一条用户反馈指令进行语义理解，
先基于 base_code（用户开始反馈时的页面快照）和 current_code（当前页面）生成 merged_pycode（新版本代码），
然后结合 incoming_code（新版本）进行三路合并，最终必须调用 emit_merge_result 一次完成合并。不要输出自由文本。

三路合并规则（重要）：
- base_code: 用户开始本次反馈时的页面快照（参考基准）
- current_code: 当前正在显示的页面代码
- merged_pycode: 你刚生成的、基于用户反馈修改后的页面代码（即 incoming）

合并策略：
1. 如果 base_code == current_code，说明页面从未被修改过，直接采纳 merged_pycode
2. 如果 current_code == merged_pycode，说明你的修改与当前一样，无需改动
3. 如果 base_code、current_code、merged_pycode 三者都不同，说明有并发冲突：
   - diff(base→current) 与 diff(base→merged) 无重叠 → 自动合并
   - diff(base→current) 与 diff(base→merged) 有重叠 → 调用 emit_merge_result 返回 ask_human
4. 无论哪种情况，最终必须调用 emit_merge_result 一次

指令格式说明：
- modify: 修改指定页面内容
- insert_before/insert_after: 在目标页前/后插入新页
- delete: 删除指定页面
- global_modify: 全局修改所有页面
- reorder: 页面重排

当用户引用了参考文件（reference_files）时：
1) 先理解参考文件中与本条反馈意图相关的内容片段
2) 将参考内容与当前页面代码进行融合，确保参考内容被正确应用到页面中
3) 如果参考文件的样式指南（style_guide）存在，请在修改时遵循该样式
4) 特别重要：当参考内容与当前页面内容冲突时，以参考文件的内容为准

请结合以下参考上下文，对当前页面代码进行融合修改：` + "\n\n" + refContext)

	user := openai.UserMessage(fmt.Sprintf(
		`三路合并上下文：
base_code（基线快照，用户开始反馈时的页面）：
---
%s
---

current_code（当前页面，正在显示的版本）：
---
%s
---

task_id=%s
page_id=%s
raw_text=%s
intents=%v`,
		baseCode,
		current.PyCode,
		req.TaskID,
		req.ViewingPageID,
		req.RawText,
		req.Intents,
	))

	msgs, err := r.chat(ctx, []openai.ChatCompletionMessageParamUnion{sys, user})
	if err != nil {
		return model.MergeResult{}, err
	}

	// 从 LLM 响应中提取它生成的 merged_pycode（即 incoming）
	extractor := r.extractor
	if extractor == nil {
		extractor = extractMergeResultFromMessages
	}
	llmResult, ok := extractor(msgs)
	if !ok {
		return model.MergeResult{}, errors.New("tool runtime did not emit valid merge result")
	}
	if llmResult.MergeStatus != "auto_resolved" && llmResult.MergeStatus != "ask_human" {
		return model.MergeResult{}, errors.New("invalid merge_status from tool runtime")
	}

	// 三路合并：以 LLM 生成的新代码作为 incoming
	incomingCode := llmResult.MergedPyCode

	threeWayIn := model.ThreeWayMergeInput{
		BaseCode:      baseCode,
		CurrentCode:   current.PyCode,
		IncomingCode:  incomingCode,
		BaseTimestamp: req.BaseTimestamp,
		PageID:        req.ViewingPageID,
	}

	// 如果注入了 MergeService，使用它进行精确三路 diff
	if r.mergeService != nil {
		result := r.mergeService.ThreeWayMerge(threeWayIn)
		// 保留 LLM 返回的问题（如果有）
		if llmResult.QuestionForUser != "" && result.MergeStatus == "ask_human" && result.QuestionForUser == "" {
			result.QuestionForUser = llmResult.QuestionForUser
		}
		return result, nil
	}

	// 兜底：基于状态比较的简单三路合并
	result := fallbackThreeWayMerge(threeWayIn)
	return result, nil
}

// fallbackThreeWayMerge 是当 MergeService 未注入时的兜底三路合并。
func fallbackThreeWayMerge(in model.ThreeWayMergeInput) model.MergeResult {
	base := strings.TrimSpace(in.BaseCode)
	current := strings.TrimSpace(in.CurrentCode)
	incoming := strings.TrimSpace(in.IncomingCode)

	if incoming == "" {
		return model.MergeResult{PageID: in.PageID, MergeStatus: "auto_resolved", MergedPyCode: current}
	}
	if base == current {
		return model.MergeResult{PageID: in.PageID, MergeStatus: "auto_resolved", MergedPyCode: incoming}
	}
	if current == incoming {
		return model.MergeResult{PageID: in.PageID, MergeStatus: "auto_resolved", MergedPyCode: current}
	}
	// 三者均不同，标记为需要人工确认
	return model.MergeResult{
		PageID:          in.PageID,
		MergeStatus:     "ask_human",
		MergedPyCode:    current,
		BaseCode:        base,
		CurrentCode:     current,
		IncomingCode:    incoming,
		QuestionForUser: "检测到同页并发修改冲突，请确认保留哪版内容。当前版本和系统推荐版本有不同的修改，请选择：",
		ConflictOpts:    []string{"keep_current 保留当前版本", "keep_incoming 采纳系统推荐", "keep_base 恢复原始版本"},
	}
}

// buildReferenceContext serializes the reference file context for injection into the LLM prompt.
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

func (m *MockToolRuntime) RunFeedbackLoop(ctx context.Context, req model.FeedbackRequest, current model.PageRenderResponse, baseCode string) (model.MergeResult, error) {
	_ = ctx
	_ = req
	_ = baseCode
	return model.MergeResult{
		MergeStatus:  "auto_resolved",
		MergedPyCode: current.PyCode + "\n# merged by mock runtime",
	}, nil
}
