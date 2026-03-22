// Package llm 封装对 tool_calling SDK 的调用，
// 提供：
//   1. 自然语言 query 意图精化（模糊 → 明确检索词）
//   2. 基于 LLM tool_calling 的逻辑冲突动态判断
package llm

import (
	"os"

	tc "toolcalling"
)

// NewAgent 从环境变量创建 tool_calling Agent
// 环境变量：LLM_API_KEY / LLM_MODEL / LLM_BASE_URL
func NewAgent() *tc.Agent {
	config := tc.LLMConfig{
		APIKey:  getEnv("LLM_API_KEY", ""),
		Model:   getEnv("LLM_MODEL", "gpt-4o-mini"),
		BaseURL: getEnv("LLM_BASE_URL", ""),
	}
	return tc.NewAgent(config)
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
