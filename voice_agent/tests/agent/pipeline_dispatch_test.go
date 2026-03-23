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

	s.AddPendingQuestion("ctx_pp", "task_pp")
	p.PostProcessResponse(context.Background(), "选A", "用户选A", false)

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

	p.PostProcessResponse(context.Background(), "测试", "好的 [REQUIREMENTS_CONFIRMED]", true)

	s.RLockReqMu()
	status := s.GetRequirements().Status
	s.RUnlockReqMu()
	if status != "confirming" {
		t.Errorf("status = %q, want confirming", status)
	}
}

func TestPostProcessResponse_TaskInit(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.PostProcessResponse(context.Background(), "帮我做PPT", `好的[TASK_INIT]{"topic":"测试"}[/TASK_INIT]`, false)

	s.RLockReqMu()
	req := s.GetRequirements()
	s.RUnlockReqMu()
	if req == nil {
		t.Fatal("Requirements should be created")
	}
}

func TestPostProcessResponse_PPTFeedback(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)
	s.RegisterTask("t1", "测试")
	s.SetActiveTask("t1")

	p.PostProcessResponse(context.Background(), "改字体", `好的[PPT_FEEDBACK]{"action_type":"style","instruction":"改字体"}[/PPT_FEEDBACK]`, false)

	calls := agent.WaitForFeedback(mock, 1)
	if len(calls) == 0 {
		t.Error("should trigger PPT feedback")
	}
}
