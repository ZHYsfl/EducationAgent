package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// tryResolveConflict
// ===========================================================================

func TestTryResolveConflict_SinglePending(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_conflict_1", "task_c1")
	s.activeTaskMu.Lock()
	s.ViewingPageID = "pg1"
	s.LastVADTimestamp = 99999
	s.activeTaskMu.Unlock()

	resolved := p.tryResolveConflict(context.Background(), "选方案A", "用户选了方案A")
	if !resolved {
		t.Error("should resolve with single pending question")
	}

	calls := waitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1 feedback, got %d", len(calls))
	}
	fb := calls[0]
	if fb.TaskID != "task_c1" {
		t.Errorf("task_id = %q", fb.TaskID)
	}
	if fb.ReplyToContextID != "ctx_conflict_1" {
		t.Errorf("reply_to_context_id = %q", fb.ReplyToContextID)
	}
	if fb.BaseTimestamp != 99999 {
		t.Errorf("base_timestamp = %d", fb.BaseTimestamp)
	}
	if fb.Intents[0].ActionType != "resolve_conflict" {
		t.Errorf("action_type = %q", fb.Intents[0].ActionType)
	}

	// Pending question should be removed
	_, ok := s.ResolvePendingQuestion("ctx_conflict_1")
	if ok {
		t.Error("pending question should have been consumed")
	}
}

func TestTryResolveConflict_MultiplePending_WithMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_a", "task_1")
	s.AddPendingQuestion("ctx_b", "task_2")

	llmResp := "好的选方案B [RESOLVE_CONFLICT:ctx_b]"
	resolved := p.tryResolveConflict(context.Background(), "选方案B", llmResp)
	if !resolved {
		t.Error("should resolve with marker")
	}

	calls := waitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1, got %d", len(calls))
	}
	if calls[0].ReplyToContextID != "ctx_b" {
		t.Errorf("should resolve ctx_b, got %q", calls[0].ReplyToContextID)
	}
}

func TestTryResolveConflict_NoPending(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	resolved := p.tryResolveConflict(context.Background(), "hi", "hello")
	if resolved {
		t.Error("should not resolve with no pending questions")
	}
}

func TestTryResolveConflict_NilClients(t *testing.T) {
	s := newTestSession(nil)
	p := newTestPipeline(s, nil)

	s.AddPendingQuestion("ctx_1", "task_1")
	resolved := p.tryResolveConflict(context.Background(), "hi", "hello")
	if resolved {
		t.Error("nil clients should return false")
	}
}

// ===========================================================================
// handleRequirementsTransition
// ===========================================================================

func TestHandleRequirementsTransition_CollectingToConfirming(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	req := makeFullRequirements()
	req.Status = "collecting"
	s.Requirements = req

	p.handleRequirementsTransition("好的信息齐全了 [REQUIREMENTS_CONFIRMED]")

	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()
	if status != "confirming" {
		t.Errorf("status = %q, want confirming", status)
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "requirements_summary")
	if !ok {
		t.Fatal("expected requirements_summary WS message")
	}
	if found.SummaryText == "" {
		t.Error("summary text should be non-empty")
	}
}

func TestHandleRequirementsTransition_CollectingStaysCollecting(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	req := NewTaskRequirements("s", "u")
	req.Topic = "测试"
	req.Status = "collecting"
	s.Requirements = req

	p.handleRequirementsTransition("知道了，请告诉我更多信息")

	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()
	if status != "collecting" {
		t.Errorf("status = %q, want collecting", status)
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "requirements_progress")
	if !ok {
		t.Fatal("expected requirements_progress WS message")
	}
	if found.Status != "collecting" {
		t.Errorf("ws status = %q", found.Status)
	}
}

func TestHandleRequirementsTransition_ConfirmingToConfirmed(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipelineWithTTS(s, mock)

	req := makeFullRequirements()
	req.SessionID = s.SessionID
	req.UserID = s.UserID
	req.Status = "confirming"
	s.Requirements = req

	registerSession(s)
	defer unregisterSession(s)

	p.handleRequirementsTransition("用户确认了 [REQUIREMENTS_CONFIRMED]")

	time.Sleep(300 * time.Millisecond)
	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()

	_ = p
	if status != "confirmed" && status != "generating" {
		t.Errorf("status = %q, want confirmed or generating", status)
	}
}

func TestHandleRequirementsTransition_ConfirmingBackToCollecting(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	req := makeFullRequirements()
	req.Status = "confirming"
	s.Requirements = req

	p.handleRequirementsTransition("我要修改一下，先改个主题")

	s.reqMu.RLock()
	status := s.Requirements.Status
	collected := s.Requirements.CollectedFields
	s.reqMu.RUnlock()
	if status != "collecting" {
		t.Errorf("status = %q, want collecting", status)
	}
	if len(collected) == 0 {
		t.Error("RefreshCollectedFields should have been called")
	}
}

func TestHandleRequirementsTransition_NilRequirements(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.handleRequirementsTransition("anything") // should not panic
}

// ===========================================================================
// buildTaskListContext
// ===========================================================================

func TestBuildTaskListContext_NoTasks(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	result := p.buildTaskListContext()
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestBuildTaskListContext_SingleTask(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.RegisterTask("task_t1", "高等数学")
	s.SetActiveTask("task_t1")

	result := p.buildTaskListContext()
	if !strings.Contains(result, "高等数学") {
		t.Error("should contain task topic")
	}
	if !strings.Contains(result, "task_t1") {
		t.Error("should contain task ID")
	}
	if !strings.Contains(result, "当前活跃") {
		t.Error("should mark active task")
	}
}

func TestBuildTaskListContext_MultipleTasks(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.RegisterTask("task_a", "数学")
	s.RegisterTask("task_b", "物理")
	s.SetActiveTask("task_a")

	result := p.buildTaskListContext()
	if !strings.Contains(result, "数学") || !strings.Contains(result, "物理") {
		t.Error("should contain both task topics")
	}
	if !strings.Contains(result, "用户可能用简称") {
		t.Error("should include multi-task instructions for LLM")
	}
}

// ===========================================================================
// buildPendingQuestionsContext
// ===========================================================================

func TestBuildPendingQuestionsContext_NoQuestions(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	result := p.buildPendingQuestionsContext()
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestBuildPendingQuestionsContext_SingleQuestion(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_q1", "task_q1")
	result := p.buildPendingQuestionsContext()
	if !strings.Contains(result, "ctx_q1") {
		t.Error("should contain context_id")
	}
}

func TestBuildPendingQuestionsContext_MultipleQuestions(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_1", "task_1")
	s.AddPendingQuestion("ctx_2", "task_2")
	result := p.buildPendingQuestionsContext()
	if !strings.Contains(result, "RESOLVE_CONFLICT") {
		t.Error("multiple questions should include RESOLVE_CONFLICT instruction")
	}
}
