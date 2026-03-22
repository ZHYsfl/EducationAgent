package main

import (
	"testing"
)

// ===========================================================================
// isSentenceEnd
// ===========================================================================

func TestIsSentenceEnd(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"你好。", true},
		{"真的吗？", true},
		{"太好了！", true},
		{"首先；", true},
		{"Hello.", true},
		{"Really?", true},
		{"Wow!", true},
		{"ok;", true},
		{"好的，", true},
		{"ok,", true},
		{"新行\n", false}, // TrimRightFunc removes \n, leaving "新行" which has no ender
		{"", false},
		{"   ", false},
		{"没有标点", false},
		{"继续说", false},
		// trailing whitespace should be trimmed
		{"好的。 ", true},
		{"hello?  \t", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isSentenceEnd(tt.input)
			if got != tt.want {
				t.Errorf("isSentenceEnd(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// truncate
// ===========================================================================

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"你好世界测试", 3, "你好世..."},
		{"abc", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// thinkFilter (streaming)
// ===========================================================================

func TestThinkFilter_BasicBlock(t *testing.T) {
	var tf thinkFilter
	out := tf.Feed("<think>internal reasoning</think>visible text")
	if out != "visible text" {
		t.Errorf("got %q, want %q", out, "visible text")
	}
	flushed := tf.Flush()
	if flushed != "" {
		t.Errorf("flush got %q, want empty", flushed)
	}
}

func TestThinkFilter_SplitAcrossTokens(t *testing.T) {
	var tf thinkFilter
	tokens := []string{"<thi", "nk>", "hidden", "</thi", "nk>", "after"}
	var result string
	for _, tok := range tokens {
		result += tf.Feed(tok)
	}
	result += tf.Flush()
	if result != "after" {
		t.Errorf("got %q, want %q", result, "after")
	}
}

func TestThinkFilter_NoThinkTags(t *testing.T) {
	var tf thinkFilter
	out := tf.Feed("plain text no tags")
	out += tf.Flush()
	if out != "plain text no tags" {
		t.Errorf("got %q, want %q", out, "plain text no tags")
	}
}

func TestThinkFilter_MultipleBlocks(t *testing.T) {
	var tf thinkFilter
	var result string
	result += tf.Feed("before<think>aaa</think>middle<think>bbb</think>end")
	result += tf.Flush()
	if result != "beforemiddleend" {
		t.Errorf("got %q, want %q", result, "beforemiddleend")
	}
}

func TestThinkFilter_UnclosedThinkDiscardedOnFlush(t *testing.T) {
	var tf thinkFilter
	result := tf.Feed("visible<think>unclosed")
	result += tf.Flush()
	if result != "visible" {
		t.Errorf("got %q, want %q", result, "visible")
	}
}

// ===========================================================================
// stripThinkTags (non-streaming)
// ===========================================================================

func TestStripThinkTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no tags", "hello world", "hello world"},
		{"single block", "<think>xyz</think>answer", "answer"},
		{"multiple blocks", "a<think>1</think>b<think>2</think>c", "abc"},
		{"unclosed", "before<think>unclosed", "before"},
		{"empty think", "<think></think>ok", "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripThinkTags(tt.input)
			if got != tt.want {
				t.Errorf("stripThinkTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// longestSuffixPrefix
// ===========================================================================

func TestLongestSuffixPrefix(t *testing.T) {
	tests := []struct {
		text string
		tag  string
		want string
	}{
		{"abc<th", "<think>", "<th"},
		{"abc<think>", "<think>", ""},
		{"abc", "<think>", ""},
		{"<", "<think>", "<"},
		{"abc</thi", "</think>", "</thi"},
		{"abc</think>", "</think>", ""},
	}
	for _, tt := range tests {
		t.Run(tt.text+"_"+tt.tag, func(t *testing.T) {
			got := longestSuffixPrefix(tt.text, tt.tag)
			if got != tt.want {
				t.Errorf("longestSuffixPrefix(%q, %q) = %q, want %q", tt.text, tt.tag, got, tt.want)
			}
		})
	}
}

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

// ===========================================================================
// SessionState String
// ===========================================================================

func TestSessionState_String(t *testing.T) {
	tests := []struct {
		state SessionState
		want  string
	}{
		{StateIdle, "idle"},
		{StateListening, "listening"},
		{StateProcessing, "processing"},
		{StateSpeaking, "speaking"},
		{SessionState(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("SessionState(%d).String() = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// AudioBuffer
// ===========================================================================

func TestAudioBuffer_WriteAndGetBlock(t *testing.T) {
	ab := NewAudioBuffer()

	data := make([]byte, BlockSize)
	for i := range data {
		data[i] = byte(i % 256)
	}
	ab.Write(data)

	block, ok := ab.GetBlock()
	if !ok {
		t.Fatal("expected to get a block")
	}
	if len(block) != BlockSize {
		t.Fatalf("block size = %d, want %d", len(block), BlockSize)
	}

	// No more blocks
	_, ok = ab.GetBlock()
	if ok {
		t.Error("should not have another block")
	}
}

func TestAudioBuffer_Flush(t *testing.T) {
	ab := NewAudioBuffer()
	ab.Write([]byte{1, 2, 3})
	flushed := ab.Flush()
	if len(flushed) != 3 {
		t.Errorf("flush len = %d, want 3", len(flushed))
	}
	if ab.Len() != 0 {
		t.Error("buffer should be empty after flush")
	}
}

func TestAudioBuffer_Reset(t *testing.T) {
	ab := NewAudioBuffer()
	ab.Write([]byte{1, 2, 3})
	ab.Reset()
	if ab.Len() != 0 {
		t.Error("buffer should be empty after reset")
	}
}

func TestAudioBuffer_FlushEmpty(t *testing.T) {
	ab := NewAudioBuffer()
	if flushed := ab.Flush(); flushed != nil {
		t.Errorf("flush of empty buffer should be nil, got %v", flushed)
	}
}

func TestAudioBuffer_PartialThenBlock(t *testing.T) {
	ab := NewAudioBuffer()
	half := BlockSize / 2
	ab.Write(make([]byte, half))
	_, ok := ab.GetBlock()
	if ok {
		t.Error("should not have a full block yet")
	}
	ab.Write(make([]byte, half))
	_, ok = ab.GetBlock()
	if !ok {
		t.Error("should have a full block now")
	}
}

// ===========================================================================
// AdaptiveController
// ===========================================================================

func TestAdaptiveController_DefaultGet(t *testing.T) {
	ac := NewAdaptiveController(DefaultChannelSizes())
	if v := ac.Get("audio_ch"); v != 200 {
		t.Errorf("audio_ch = %d, want 200", v)
	}
	if v := ac.Get("unknown"); v != 20 {
		t.Errorf("unknown = %d, want 20", v)
	}
}

func TestAdaptiveController_RecordAndAdjust_HighUtil(t *testing.T) {
	ac := NewAdaptiveController(ChannelSizes{
		AudioCh:     100,
		ASRAudioCh:  20,
		ASRResultCh: 20,
		SentenceCh:  20,
		WriteCh:     100,
		TTSChunkCh:  20,
	})
	for i := 0; i < 5; i++ {
		ac.RecordLen("audio_ch", 90)
	}
	ac.Adjust()
	if v := ac.Get("audio_ch"); v <= 100 {
		t.Errorf("expected audio_ch > 100 after high utilization, got %d", v)
	}
}

func TestAdaptiveController_RecordAndAdjust_LowUtil(t *testing.T) {
	ac := NewAdaptiveController(ChannelSizes{
		AudioCh:     400,
		ASRAudioCh:  20,
		ASRResultCh: 20,
		SentenceCh:  20,
		WriteCh:     100,
		TTSChunkCh:  20,
	})
	ac.RecordLen("audio_ch", 10)
	ac.Adjust()
	if v := ac.Get("audio_ch"); v >= 400 {
		t.Errorf("expected audio_ch < 400 after low utilization, got %d", v)
	}
}

func TestAdaptiveController_ClampMin(t *testing.T) {
	ac := NewAdaptiveController(ChannelSizes{
		AudioCh:     50,
		ASRAudioCh:  5,
		ASRResultCh: 5,
		SentenceCh:  5,
		WriteCh:     64,
		TTSChunkCh:  5,
	})
	ac.RecordLen("audio_ch", 1)
	ac.Adjust()
	if v := ac.Get("audio_ch"); v < 50 {
		t.Errorf("expected audio_ch >= 50 (min), got %d", v)
	}
}

// ===========================================================================
// ConversationHistory
// ===========================================================================

func TestConversationHistory_AddAndRetrieve(t *testing.T) {
	h := NewConversationHistory("system")
	h.AddUser("hello")
	h.AddAssistant("hi there")
	h.AddInterruptedAssistant("partial")

	msgs := h.ToOpenAI()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (system + 3), got %d", len(msgs))
	}
}

func TestConversationHistory_ToOpenAIWithThoughtAndPrompt(t *testing.T) {
	h := NewConversationHistory("default system")
	h.AddUser("question")

	msgs := h.ToOpenAIWithThoughtAndPrompt("draft thought", "custom system")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
}

func TestConversationHistory_ToOpenAIWithDraftAndThought(t *testing.T) {
	h := NewConversationHistory("sys")
	h.AddUser("prev question")
	h.AddAssistant("prev answer")

	msgs := h.ToOpenAIWithDraftAndThought("partial text", "some thinking")
	// system + prev_user + prev_assistant + draft_user + draft_assistant_thought
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}
}

func TestConversationHistory_NoPreviousThought(t *testing.T) {
	h := NewConversationHistory("sys")
	h.AddUser("prev")

	msgs := h.ToOpenAIWithDraftAndThought("partial", "")
	// system + prev_user + draft_user (no thought)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

// ===========================================================================
// decodeAPIData
// ===========================================================================

func TestDecodeAPIData_WrappedSuccess(t *testing.T) {
	raw := []byte(`{"code":200,"message":"ok","data":{"task_id":"t1"}}`)
	var out PPTInitResponse
	err := decodeAPIData(raw, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID != "t1" {
		t.Errorf("task_id = %q, want t1", out.TaskID)
	}
}

func TestDecodeAPIData_WrappedError(t *testing.T) {
	raw := []byte(`{"code":50200,"message":"服务不可用"}`)
	var out PPTInitResponse
	err := decodeAPIData(raw, &out)
	if err == nil {
		t.Fatal("expected error for non-200 code")
	}
}

func TestDecodeAPIData_NilOut(t *testing.T) {
	err := decodeAPIData([]byte(`{"code":200}`), nil)
	if err != nil {
		t.Fatalf("nil out should not error, got %v", err)
	}
}

func TestDecodeAPIData_RawJSON(t *testing.T) {
	raw := []byte(`{"task_id":"direct"}`)
	var out PPTInitResponse
	err := decodeAPIData(raw, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.TaskID != "direct" {
		t.Errorf("task_id = %q, want direct", out.TaskID)
	}
}

func TestDecodeAPIData_NullData(t *testing.T) {
	raw := []byte(`{"code":200,"data":null}`)
	var out PPTInitResponse
	err := decodeAPIData(raw, &out)
	if err != nil {
		t.Fatalf("null data should not error, got %v", err)
	}
}

// ===========================================================================
// ChannelSizes load/clamp
// ===========================================================================

func TestLoadChannelSizes_Fallback(t *testing.T) {
	sizes := LoadChannelSizes("/nonexistent/path.json", DefaultChannelSizes())
	if sizes.AudioCh != 200 {
		t.Errorf("expected fallback AudioCh=200, got %d", sizes.AudioCh)
	}
}

func TestClampSizes(t *testing.T) {
	s := ChannelSizes{
		AudioCh:     1,
		ASRAudioCh:  1000,
		ASRResultCh: 20,
		SentenceCh:  20,
		WriteCh:     100,
		TTSChunkCh:  20,
	}
	clamped := clampSizes(s)
	if clamped.AudioCh < 50 {
		t.Errorf("AudioCh should be clamped to min 50, got %d", clamped.AudioCh)
	}
	if clamped.ASRAudioCh > 80 {
		t.Errorf("ASRAudioCh should be clamped to max 80, got %d", clamped.ASRAudioCh)
	}
}

// ===========================================================================
// string helpers
// ===========================================================================

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
