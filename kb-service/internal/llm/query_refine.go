package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	tc "toolcalling"
)

// QueryRefiner 使用 LLM 动态解析高度模糊的自然语言 query，
// 将其扩写为更精确的检索意图，避免用正则/硬编码处理自然语言。
//
// 例：
//   输入："帮我找找那个三角形的东西"
//   输出："三角形定义 三角形内角和 三角形面积公式 几何图形"
type QueryRefiner struct {
	agent *tc.Agent
}

// NewQueryRefiner 创建 QueryRefiner，复用传入的 Agent（避免重复初始化）
func NewQueryRefiner(agent *tc.Agent) *QueryRefiner {
	return &QueryRefiner{agent: agent}
}

// Refine 对输入 query 做意图精化。
// 若 LLM 不可用或返回为空，直接返回原始 query（降级，不阻断主流程）。
func (r *QueryRefiner) Refine(ctx context.Context, query string) string {
	if r.agent == nil || strings.TrimSpace(query) == "" {
		return query
	}

	// 用 ChatText（无 tool_calling），轻量快速
	prompt := buildRefinePrompt(query)
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(
			"你是一个教育领域知识库检索助手。" +
				"用户输入的查询可能非常口语化、模糊或不完整。" +
				"请将用户的查询意图精化为2-5个精准的中文检索关键词或短语，用空格分隔。" +
				"只输出关键词，不要解释，不要标点。",
		),
		openai.UserMessage(prompt),
	}

	refined, err := r.agent.ChatText(ctx, messages)
	if err != nil || strings.TrimSpace(refined) == "" {
		// 降级：返回原始 query，不报错
		return query
	}
	return strings.TrimSpace(refined)
}

func buildRefinePrompt(query string) string {
	return fmt.Sprintf("用户查询：\"%s\"\n请输出精化后的检索关键词：", query)
}
