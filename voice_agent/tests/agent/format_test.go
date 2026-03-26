package agent_test

import (
	agent "voiceagent/agent"
	"testing"
)

// ===========================================================================
// FormatContextForLLM
// ===========================================================================

func TestFormatContextForLLM_Empty(t *testing.T) {
	result := agent.FormatContextForLLM(nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestFormatContextForLLM_NonEmpty(t *testing.T) {
	msgs := []agent.ContextMessage{
		{Source: "knowledge_base", MsgType: "kb_summary", Content: "知识点A"},
		{Source: "web_search", MsgType: "search_result", Content: "搜索结果B"},
	}
	result := agent.FormatContextForLLM(msgs)
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "知识点A") || !contains(result, "搜索结果B") {
		t.Errorf("result missing content: %s", result)
	}
	if !contains(result, "knowledge_base") || !contains(result, "web_search") {
		t.Errorf("result missing source labels: %s", result)
	}
}

// ===========================================================================
// formatMemoryForLLM
// ===========================================================================

func TestFormatMemoryForLLM_Empty(t *testing.T) {
	result := agent.FormatMemoryForLLM(agent.MemoryRecallResponse{})
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestFormatMemoryForLLM_WithFacts(t *testing.T) {
	resp := agent.MemoryRecallResponse{
		Facts:          []agent.MemoryEntry{{Content: "用户喜欢蓝色"}},
		Preferences:    []agent.MemoryEntry{{Content: "简洁风格"}},
		ProfileSummary: "数学教师",
	}
	result := agent.FormatMemoryForLLM(resp)
	if !contains(result, "用户喜欢蓝色") || !contains(result, "简洁风格") || !contains(result, "数学教师") {
		t.Errorf("missing content: %s", result)
	}
}

func TestFormatMemoryForLLM_EmptyContentUsesValue(t *testing.T) {
	resp := agent.MemoryRecallResponse{
		Facts: []agent.MemoryEntry{{Value: "fallback_value"}},
	}
	result := agent.FormatMemoryForLLM(resp)
	if !contains(result, "fallback_value") {
		t.Errorf("should use Value when Content is empty: %s", result)
	}
}

// ===========================================================================
// formatSearchForLLM
// ===========================================================================

func TestFormatSearchForLLM_Empty(t *testing.T) {
	result := agent.FormatSearchForLLM(agent.SearchResponse{})
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestFormatSearchForLLM_NonEmpty(t *testing.T) {
	resp := agent.SearchResponse{
		Summary: "概要信息",
		Results: []agent.SearchResult{{Title: "标题A", URL: "http://a.com", Snippet: "摘要A"}},
	}
	result := agent.FormatSearchForLLM(resp)
	if !contains(result, "概要信息") || !contains(result, "标题A") || !contains(result, "http://a.com") {
		t.Errorf("missing content: %s", result)
	}
}

// ===========================================================================
// NewID
// ===========================================================================

func TestNewID_Prefix(t *testing.T) {
	id := agent.NewID("sess_")
	if !hasPrefix(id, "sess_") {
		t.Errorf("expected prefix 'sess_', got %q", id)
	}
	if len(id) < 6 {
		t.Errorf("id too short: %q", id)
	}
}

func TestNewID_Unique(t *testing.T) {
	a := agent.NewID("x_")
	b := agent.NewID("x_")
	if a == b {
		t.Error("two calls should produce unique IDs")
	}
}
