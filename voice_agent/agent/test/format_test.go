package agent

import (
	"testing"
)

// ===========================================================================
// FormatContextForLLM
// ===========================================================================

func TestFormatContextForLLM_Empty(t *testing.T) {
	result := FormatContextForLLM(nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestFormatContextForLLM_NonEmpty(t *testing.T) {
	msgs := []ContextMessage{
		{Source: "knowledge_base", MsgType: "rag_chunks", Content: "知识点A"},
		{Source: "web_search", MsgType: "search_result", Content: "搜索结果B"},
	}
	result := FormatContextForLLM(msgs)
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
// formatChunksForLLM
// ===========================================================================

func TestFormatChunksForLLM_Empty(t *testing.T) {
	result := formatChunksForLLM(nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestFormatChunksForLLM_NonEmpty(t *testing.T) {
	chunks := []RetrievedChunk{
		{ChunkID: "c1", DocTitle: "线性代数", Content: "矩阵乘法", Score: 0.85},
		{ChunkID: "c2", DocTitle: "微积分", Content: "求导公式", Score: 0.72},
	}
	result := formatChunksForLLM(chunks)
	if !contains(result, "矩阵乘法") || !contains(result, "求导公式") {
		t.Errorf("missing content: %s", result)
	}
}

// ===========================================================================
// formatMemoryForLLM
// ===========================================================================

func TestFormatMemoryForLLM_Empty(t *testing.T) {
	result := formatMemoryForLLM(MemoryRecallResponse{})
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestFormatMemoryForLLM_WithFacts(t *testing.T) {
	resp := MemoryRecallResponse{
		Facts:          []MemoryEntry{{Content: "用户喜欢蓝色"}},
		Preferences:    []MemoryEntry{{Content: "简洁风格"}},
		ProfileSummary: "数学教师",
	}
	result := formatMemoryForLLM(resp)
	if !contains(result, "用户喜欢蓝色") || !contains(result, "简洁风格") || !contains(result, "数学教师") {
		t.Errorf("missing content: %s", result)
	}
}

func TestFormatMemoryForLLM_EmptyContentUsesValue(t *testing.T) {
	resp := MemoryRecallResponse{
		Facts: []MemoryEntry{{Value: "fallback_value"}},
	}
	result := formatMemoryForLLM(resp)
	if !contains(result, "fallback_value") {
		t.Errorf("should use Value when Content is empty: %s", result)
	}
}

// ===========================================================================
// formatSearchForLLM
// ===========================================================================

func TestFormatSearchForLLM_Empty(t *testing.T) {
	result := formatSearchForLLM(SearchResponse{})
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestFormatSearchForLLM_NonEmpty(t *testing.T) {
	resp := SearchResponse{
		Summary: "概要信息",
		Results: []SearchResult{{Title: "标题A", URL: "http://a.com", Snippet: "摘要A"}},
	}
	result := formatSearchForLLM(resp)
	if !contains(result, "概要信息") || !contains(result, "标题A") || !contains(result, "http://a.com") {
		t.Errorf("missing content: %s", result)
	}
}

// ===========================================================================
// NewID
// ===========================================================================

func TestNewID_Prefix(t *testing.T) {
	id := NewID("sess_")
	if !hasPrefix(id, "sess_") {
		t.Errorf("expected prefix 'sess_', got %q", id)
	}
	if len(id) < 6 {
		t.Errorf("id too short: %q", id)
	}
}

func TestNewID_Unique(t *testing.T) {
	a := NewID("x_")
	b := NewID("x_")
	if a == b {
		t.Error("two calls should produce unique IDs")
	}
}
