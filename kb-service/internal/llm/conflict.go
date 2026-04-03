package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	tc "toolcalling"
)

// ConflictResult LLM 逻辑冲突判断结果
type ConflictResult struct {
	IsConflict  bool   `json:"is_conflict"`
	Reason      string `json:"reason"`       // 冲突说明（供日志/前端展示）
	Suggestion  string `json:"suggestion"`   // LLM 建议的解决方案
}

// ConflictDetector 使用 LLM tool_calling 动态判断两条指令是否存在逻辑冲突。
// 不使用任何硬编码规则，完全由 LLM 根据语义上下文判断。
type ConflictDetector struct {
	agent *tc.Agent
}

// NewConflictDetector 创建 ConflictDetector，复用传入的 Agent
func NewConflictDetector(agent *tc.Agent) *ConflictDetector {
	cd := &ConflictDetector{agent: agent}
	cd.registerTools()
	return cd
}

// registerTools 注册 report_conflict tool，让 LLM 通过 tool_calling 结构化输出结果
func (cd *ConflictDetector) registerTools() {
	cd.agent.AddTool(tc.Tool{
		Name:        "report_conflict_result",
		Description: "报告两条指令是否存在逻辑冲突。必须调用此工具返回判断结果，不允许直接文字回复。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"is_conflict": map[string]any{
					"type":        "boolean",
					"description": "两条指令是否存在逻辑冲突",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "如果有冲突，说明冲突的具体原因；无冲突填空字符串",
				},
				"suggestion": map[string]any{
					"type":        "string",
					"description": "如果有冲突，建议如何解决；无冲突填空字符串",
				},
			},
			"required": []any{"is_conflict", "reason", "suggestion"},
		},
		Function: func(_ context.Context, args map[string]any) (string, error) {
			// tool 的实际执行：把参数序列化回 JSON 供主流程读取
			out, err := json.Marshal(args)
			if err != nil {
				return "", err
			}
			return string(out), nil
		},
	})
}

// Detect 判断 instructionA 和 instructionB 是否存在逻辑冲突。
// 完全由 LLM 动态判断，不依赖任何硬编码规则。
// 若 LLM 不可用，降级返回 IsConflict=false（不误判，让主流程继续）。
func (cd *ConflictDetector) Detect(
	ctx context.Context,
	instructionA, instructionB string,
) ConflictResult {
	if cd.agent == nil {
		return ConflictResult{}
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(
			"你是一个教育内容逻辑分析专家。" +
				"你的任务是判断两条课件生成指令是否存在逻辑冲突（如互相矛盾、无法同时满足）。" +
				"你必须调用 report_conflict_result 工具返回判断结果，禁止直接文字回复。",
		),
		openai.UserMessage(fmt.Sprintf(
			"请判断以下两条指令是否存在逻辑冲突：\n\n"+
				"指令A：%s\n\n"+
				"指令B：%s\n\n"+
				"请调用 report_conflict_result 工具返回结果。",
			instructionA, instructionB,
		)),
	}

	resultMsgs, err := cd.agent.Chat(ctx, messages)
	if err != nil {
		// 降级：LLM 不可用时不误判为冲突
		return ConflictResult{Reason: fmt.Sprintf("LLM不可用，跳过冲突检测: %v", err)}
	}

	// 从 tool 调用结果中提取 JSON
	return extractConflictResult(resultMsgs)
}

// DetectMulti 判断新指令与已有指令列表中是否存在任意冲突
func (cd *ConflictDetector) DetectMulti(
	ctx context.Context,
	newInstruction string,
	existing []string,
) ConflictResult {
	if len(existing) == 0 {
		return ConflictResult{}
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(
			"你是一个教育内容逻辑分析专家。" +
				"你的任务是判断新指令与已有指令列表中是否存在逻辑冲突。" +
				"你必须调用 report_conflict_result 工具返回判断结果，禁止直接文字回复。",
		),
		openai.UserMessage(fmt.Sprintf(
			"新指令：%s\n\n已有指令列表：\n%s\n\n请调用 report_conflict_result 工具返回结果。",
			newInstruction,
			numberedList(existing),
		)),
	}

	resultMsgs, err := cd.agent.Chat(ctx, messages)
	if err != nil {
		return ConflictResult{Reason: fmt.Sprintf("LLM不可用，跳过冲突检测: %v", err)}
	}
	return extractConflictResult(resultMsgs)
}

// ── 内部辅助 ─────────────────────────────────────────────────────────────────

// extractConflictResult 从 Chat 返回的消息链中提取 tool 调用结果
func extractConflictResult(msgs []openai.ChatCompletionMessageParamUnion) ConflictResult {
	// 从后往前找 tool 消息（OfTool）
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.OfTool == nil {
			continue
		}
		content := ""
		if m.OfTool.Content.OfString.Valid() {
			content = m.OfTool.Content.OfString.Value
		}
		if content == "" {
			continue
		}
		var result ConflictResult
		if err := json.Unmarshal([]byte(content), &result); err == nil {
			return result
		}
	}
	// 没找到 tool 结果，降级
	return ConflictResult{}
}

func numberedList(items []string) string {
	var sb strings.Builder
	for i, s := range items {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
	}
	return sb.String()
}
