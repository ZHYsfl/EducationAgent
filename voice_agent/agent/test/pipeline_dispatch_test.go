package agent

import (
	"context"
	"testing"
)

// ===========================================================================
// postProcessResponse dispatching
// ===========================================================================

func TestPostProcessResponse_ConflictFirst(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_pp", "task_pp")
	p.postProcessResponse(context.Background(), "选A", "用户选A", false)

	calls := waitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Error("conflict resolution should take priority")
	}
}

func TestPostProcessResponse_RequirementsMode(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	req := makeFullRequirements()
	req.Status = "collecting"
	s.Requirements = req

	p.postProcessResponse(context.Background(), "测试", "好的 [REQUIREMENTS_CONFIRMED]", true)

	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()
	if status != "confirming" {
		t.Errorf("status = %q, want confirming", status)
	}
}

func TestPostProcessResponse_TaskInit(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.postProcessResponse(context.Background(), "帮我做PPT", `好的[TASK_INIT]{"topic":"测试"}[/TASK_INIT]`, false)

	s.reqMu.RLock()
	req := s.Requirements
	s.reqMu.RUnlock()
	if req == nil {
		t.Fatal("Requirements should be created")
	}
}

func TestPostProcessResponse_PPTFeedback(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	s.RegisterTask("t1", "测试")
	s.SetActiveTask("t1")

	p.postProcessResponse(context.Background(), "改字体", `好的[PPT_FEEDBACK]{"action_type":"style","instruction":"改字体"}[/PPT_FEEDBACK]`, false)

	calls := waitForFeedback(mock, 1)
	if len(calls) == 0 {
		t.Error("should trigger PPT feedback")
	}
}
