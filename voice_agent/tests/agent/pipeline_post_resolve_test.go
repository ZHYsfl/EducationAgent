package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"strings"
	"testing"
)

// ===========================================================================
// tryResolveConflict
// ===========================================================================

func TestTryResolveConflict_SinglePending(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_conflict_1", "task_c1")
	s.LockActiveTaskMu()
	s.ViewingPageID = "pg1"
	s.LastVADTimestamp = 99999
	s.UnlockActiveTaskMu()

	resolved := p.TryResolveConflict(context.Background(), "选方案A", "用户选了方案A")
	if !resolved {
		t.Error("should resolve with single pending question")
	}

	calls := agent.WaitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1 feedback, got %d", len(calls))
	}
	fb := calls[0]
	if fb.TaskID != "task_c1" {
		t.Errorf("task_id = %q", fb.TaskID)
	}
	if fb.BaseTimestamp != 99999 {
		t.Errorf("base_timestamp = %d", fb.BaseTimestamp)
	}
	if fb.RawText != "选方案A" {
		t.Errorf("raw_text = %q", fb.RawText)
	}
	if fb.Intents != nil {
		t.Errorf("Intents should be nil, got %v", fb.Intents)
	}

	// Pending question should be removed
	_, ok := s.ResolvePendingQuestion("ctx_conflict_1")
	if ok {
		t.Error("pending question should have been consumed")
	}
}

func TestTryResolveConflict_MultiplePending_WithMarker(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_a", "task_1")
	s.AddPendingQuestion("ctx_b", "task_2")

	llmResp := "好的选方案B [RESOLVE_CONFLICT:ctx_b]"
	resolved := p.TryResolveConflict(context.Background(), "选方案B", llmResp)
	if !resolved {
		t.Error("should resolve with marker")
	}

	calls := agent.WaitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1, got %d", len(calls))
	}
	if calls[0].TaskID != "task_2" {
		t.Errorf("should resolve ctx_b's task (task_2), got %q", calls[0].TaskID)
	}
}

func TestTryResolveConflict_NoPending(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	resolved := p.TryResolveConflict(context.Background(), "hi", "hello")
	if resolved {
		t.Error("should not resolve with no pending questions")
	}
}

func TestTryResolveConflict_NilClients(t *testing.T) {
	s := agent.NewTestSession(nil)
	p := agent.NewTestPipeline(s, nil)

	s.AddPendingQuestion("ctx_1", "task_1")
	resolved := p.TryResolveConflict(context.Background(), "hi", "hello")
	if resolved {
		t.Error("nil clients should return false")
	}
}


// ===========================================================================
// buildTaskListContext
// ===========================================================================

func TestBuildTaskListContext_NoTasks(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	result := p.BuildTaskListContext()
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestBuildTaskListContext_SingleTask(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.RegisterTask("task_t1", "高等数学")
	s.SetActiveTask("task_t1")

	result := p.BuildTaskListContext()
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
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.RegisterTask("task_a", "数学")
	s.RegisterTask("task_b", "物理")
	s.SetActiveTask("task_a")

	result := p.BuildTaskListContext()
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
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	result := p.BuildPendingQuestionsContext()
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestBuildPendingQuestionsContext_SingleQuestion(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_q1", "task_q1")
	result := p.BuildPendingQuestionsContext()
	if !strings.Contains(result, "ctx_q1") {
		t.Error("should contain context_id")
	}
}

func TestBuildPendingQuestionsContext_MultipleQuestions(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_1", "task_1")
	s.AddPendingQuestion("ctx_2", "task_2")
	result := p.BuildPendingQuestionsContext()
	if !strings.Contains(result, "RESOLVE_CONFLICT") {
		t.Error("multiple questions should include RESOLVE_CONFLICT instruction")
	}
}
