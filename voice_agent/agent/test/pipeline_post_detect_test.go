package agent

import (
	"testing"
	"time"
)

// ===========================================================================
// extractContextIDFromResponse
// ===========================================================================

func TestExtractContextIDFromResponse(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with marker", "用户确认了方案A [RESOLVE_CONFLICT:ctx_abc123]", "ctx_abc123"},
		{"no marker", "普通回复没有标记", ""},
		{"marker at end", "[RESOLVE_CONFLICT:ctx_xyz]", "ctx_xyz"},
		{"with spaces", "[RESOLVE_CONFLICT: ctx_spaced ]", "ctx_spaced"},
		{"unclosed", "[RESOLVE_CONFLICT:ctx_unclosed", ""},
		{"empty id", "[RESOLVE_CONFLICT:]", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractContextIDFromResponse(tt.input)
			if got != tt.want {
				t.Errorf("extractContextIDFromResponse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// tryDetectTaskInit
// ===========================================================================

func TestTryDetectTaskInit_WithMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	llmResp := `好的，我来帮您制作课件。[TASK_INIT]{"topic":"高等数学"}[/TASK_INIT]`
	detected := p.tryDetectTaskInit(llmResp)
	if !detected {
		t.Fatal("should detect TASK_INIT")
	}

	s.reqMu.RLock()
	req := s.Requirements
	s.reqMu.RUnlock()

	if req == nil {
		t.Fatal("Requirements should be created")
	}
	if req.Topic != "高等数学" {
		t.Errorf("topic = %q, want 高等数学", req.Topic)
	}
	if req.Status != "collecting" {
		t.Errorf("status = %q, want collecting", req.Status)
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "requirements_progress")
	if !ok {
		t.Error("expected requirements_progress WS message")
	}
	if found.Status != "collecting" {
		t.Errorf("ws status = %q", found.Status)
	}
}

func TestTryDetectTaskInit_NoMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	detected := p.tryDetectTaskInit("普通对话没有任何标记")
	if detected {
		t.Error("should not detect TASK_INIT")
	}
}

func TestTryDetectTaskInit_InvalidJSON(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	detected := p.tryDetectTaskInit(`[TASK_INIT]not valid json[/TASK_INIT]`)
	if detected {
		t.Error("invalid JSON should return false")
	}
	if s.Requirements != nil {
		t.Error("Requirements should not be set for invalid JSON")
	}
}

func TestTryDetectTaskInit_EmptyTopic(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	detected := p.tryDetectTaskInit(`[TASK_INIT]{"topic":""}[/TASK_INIT]`)
	if !detected {
		t.Error("should still detect even with empty topic")
	}
	s.reqMu.RLock()
	req := s.Requirements
	s.reqMu.RUnlock()
	if req.Topic != "" {
		t.Errorf("topic should be empty, got %q", req.Topic)
	}
}

// ===========================================================================
// trySendPPTFeedback
// ===========================================================================

func TestTrySendPPTFeedback_WithMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.RegisterTask("task_fb", "数学")
	s.SetActiveTask("task_fb")
	s.activeTaskMu.Lock()
	s.ViewingPageID = "page_fb"
	s.LastVADTimestamp = 12345
	s.activeTaskMu.Unlock()

	llmResp := `好的，我帮您修改。[PPT_FEEDBACK]{"action_type":"modify","page_id":"page_fb","instruction":"字体改大","scope":"page","keywords":["字体"]}[/PPT_FEEDBACK]`
	p.trySendPPTFeedback("把字体改大一点", llmResp)

	calls := waitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1 feedback call, got %d", len(calls))
	}
	fb := calls[0]
	if fb.TaskID != "task_fb" {
		t.Errorf("task_id = %q", fb.TaskID)
	}
	if fb.BaseTimestamp != 12345 {
		t.Errorf("base_timestamp = %d, want 12345", fb.BaseTimestamp)
	}
	if fb.ViewingPageID != "page_fb" {
		t.Errorf("viewing_page_id = %q", fb.ViewingPageID)
	}
	if len(fb.Intents) != 1 {
		t.Fatalf("expected 1 intent, got %d", len(fb.Intents))
	}
	intent := fb.Intents[0]
	if intent.ActionType != "modify" {
		t.Errorf("action_type = %q", intent.ActionType)
	}
	if intent.Instruction != "字体改大" {
		t.Errorf("instruction = %q", intent.Instruction)
	}
}

func TestTrySendPPTFeedback_NoMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	s.RegisterTask("t1", "数学")
	s.SetActiveTask("t1")

	p.trySendPPTFeedback("你好", "你好，有什么可以帮您的吗？")
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := len(mock.feedbackCalls)
	mock.mu.Unlock()
	if calls != 0 {
		t.Error("should not send feedback without marker")
	}
}

func TestTrySendPPTFeedback_NoActiveTask(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.trySendPPTFeedback("改一下", `[PPT_FEEDBACK]{"action_type":"modify"}[/PPT_FEEDBACK]`)
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := len(mock.feedbackCalls)
	mock.mu.Unlock()
	if calls != 0 {
		t.Error("should not send feedback without active task")
	}
}

func TestTrySendPPTFeedback_InvalidJSON(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	s.RegisterTask("t1", "数学")
	s.SetActiveTask("t1")

	p.trySendPPTFeedback("改一下", `[PPT_FEEDBACK]invalid json[/PPT_FEEDBACK]`)
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := len(mock.feedbackCalls)
	mock.mu.Unlock()
	if calls != 0 {
		t.Error("invalid JSON should not trigger feedback")
	}
}

func TestTrySendPPTFeedback_FallbackPageID(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	s.RegisterTask("t1", "数学")
	s.SetActiveTask("t1")
	s.activeTaskMu.Lock()
	s.ViewingPageID = "current_page"
	s.activeTaskMu.Unlock()

	p.trySendPPTFeedback("改背景", `[PPT_FEEDBACK]{"action_type":"style","instruction":"改背景色"}[/PPT_FEEDBACK]`)
	calls := waitForFeedback(mock, 1)
	if len(calls) == 0 {
		t.Fatal("expected feedback call")
	}
	if calls[0].Intents[0].PageID != "current_page" {
		t.Errorf("should fallback to viewing page, got %q", calls[0].Intents[0].PageID)
	}
}
