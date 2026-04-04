package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"testing"
)

// ===========================================================================
// postProcessResponse dispatching
// ===========================================================================

func TestPostProcessResponse_ConflictFirst(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_pp", "task_pp", "", 0, "test question")
	p.PostProcessResponse(context.Background(), "选A", "用户选A", nil)

	calls := agent.WaitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Error("conflict resolution should take priority")
	}
}

func TestPostProcessResponse_RequirementsMode(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	req := makeFullRequirements()
	req.Status = "collecting"
	s.SetRequirements(req)

	p.PostProcessResponse(context.Background(), "测试", "好的", nil)

	s.RLockReqMu()
	status := s.GetRequirements().Status
	s.RUnlockReqMu()
	if status != "collecting" {
		t.Errorf("status = %q, want collecting", status)
	}
}


