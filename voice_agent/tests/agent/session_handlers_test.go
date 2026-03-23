package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// handleTaskInit
// ===========================================================================

func TestHandleTaskInit(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)

	s.HandleTaskInit(agent.WSMessage{
		Type:        "task_init",
		Topic:       "线性代数",
		TotalPages:  20,
		Audience:    "大二学生",
		GlobalStyle: "蓝色简洁",
		Description: "额外说明",
	})

	s.RLockReqMu()
	req := s.GetRequirements()
	s.RUnlockReqMu()

	if req == nil {
		t.Fatal("Requirements should be set")
	}
	if req.Topic != "线性代数" {
		t.Errorf("topic = %q, want 线性代数", req.Topic)
	}
	if req.TotalPages != 20 {
		t.Errorf("total_pages = %d, want 20", req.TotalPages)
	}
	if req.TargetAudience != "大二学生" {
		t.Errorf("audience = %q", req.TargetAudience)
	}
	if req.GlobalStyle != "蓝色简洁" {
		t.Errorf("style = %q", req.GlobalStyle)
	}
	if req.AdditionalNotes != "额外说明" {
		t.Errorf("notes = %q", req.AdditionalNotes)
	}
	if req.Status != "collecting" {
		t.Errorf("status = %q, want collecting", req.Status)
	}

	msgs := agent.DrainWriteCh(s)
	found, ok := agent.FindWSMessage(msgs, "requirements_progress")
	if !ok {
		t.Fatal("expected requirements_progress message")
	}
	if found.Status != "collecting" {
		t.Errorf("WS status = %q", found.Status)
	}
}

// ===========================================================================
// handleRequirementsConfirm
// ===========================================================================

func TestHandleRequirementsConfirm_Confirm(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)

	req := makeFullRequirements()
	req.SessionID = s.SessionID
	req.UserID = s.UserID
	req.Status = "confirming"
	s.SetRequirements(req)

	confirmed := true
	s.HandleRequirementsConfirm(agent.WSMessage{Confirmed: &confirmed})

	time.Sleep(50 * time.Millisecond)
	s.RLockReqMu()
	status := s.GetRequirements().Status
	s.RUnlockReqMu()

	if status != "confirmed" && status != "generating" {
		t.Errorf("status = %q, want confirmed or generating", status)
	}
}

func TestHandleRequirementsConfirm_Unconfirm(t *testing.T) {
	s := agent.NewTestSession(nil)
	req := makeFullRequirements()
	req.Status = "confirming"
	s.SetRequirements(req)

	notConfirmed := false
	s.HandleRequirementsConfirm(agent.WSMessage{
		Confirmed:     &notConfirmed,
		Modifications: "把颜色改成红色",
	})

	s.RLockReqMu()
	defer s.RUnlockReqMu()
	if s.GetRequirements().Status != "collecting" {
		t.Errorf("status = %q, want collecting", s.GetRequirements().Status)
	}
	if !strings.Contains(s.GetRequirements().AdditionalNotes, "红色") {
		t.Error("modifications not appended")
	}
}

func TestHandleRequirementsConfirm_NilRequirements(t *testing.T) {
	s := agent.NewTestSession(nil)
	confirmed := true
	s.HandleRequirementsConfirm(agent.WSMessage{Confirmed: &confirmed})
	// should not panic
}

// ===========================================================================
// publishVADEvent
// ===========================================================================

func TestPublishVADEvent_WithActiveTask(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	s.SetActiveTask("task_vad")
	s.LockActiveTaskMu()
	s.ViewingPageID = "page_vad"
	s.UnlockActiveTaskMu()

	s.PublishVADEvent()
	time.Sleep(50 * time.Millisecond)

	mock.Mu.Lock()
	calls := mock.VADEventCalls
	mock.Mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 VAD event, got %d", len(calls))
	}
	if calls[0].TaskID != "task_vad" {
		t.Errorf("task_id = %q", calls[0].TaskID)
	}
	if calls[0].ViewingPageID != "page_vad" {
		t.Errorf("viewing_page_id = %q", calls[0].ViewingPageID)
	}
	if calls[0].Timestamp == 0 {
		t.Error("timestamp should be non-zero")
	}
	if s.GetLastVADTimestamp() == 0 {
		t.Error("LastVADTimestamp should be set")
	}
}

func TestPublishVADEvent_NoActiveTask(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)

	s.PublishVADEvent()
	time.Sleep(30 * time.Millisecond)

	mock.Mu.Lock()
	calls := mock.VADEventCalls
	mock.Mu.Unlock()

	if len(calls) != 0 {
		t.Errorf("should not send VAD event without active task, got %d", len(calls))
	}
}

func TestPublishVADEvent_NilClients(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.SetActiveTask("task_1")
	s.PublishVADEvent() // should not panic
	if s.GetLastVADTimestamp() == 0 {
		t.Error("timestamp should still be recorded even without clients")
	}
}

// ===========================================================================
// prefillFromMemory
// ===========================================================================

func TestPrefillFromMemory_FillsEmptyFields(t *testing.T) {
	mock := &agent.MockServices{
		GetUserProfileFn: func(ctx context.Context, userID string) (agent.UserProfile, error) {
			return agent.UserProfile{
				Subject:           "数学",
				VisualPreferences: map[string]string{"color_scheme": "蓝色"},
			}, nil
		},
	}
	s := agent.NewTestSession(mock)
	req := agent.NewTaskRequirements(s.SessionID, s.UserID)
	s.PrefillFromMemory(req)

	if !strings.Contains(req.TargetAudience, "数学") {
		t.Errorf("target_audience = %q, expected to contain 数学", req.TargetAudience)
	}
	if req.GlobalStyle != "蓝色" {
		t.Errorf("global_style = %q, want 蓝色", req.GlobalStyle)
	}
}

func TestPrefillFromMemory_DoesNotOverwrite(t *testing.T) {
	mock := &agent.MockServices{
		GetUserProfileFn: func(ctx context.Context, userID string) (agent.UserProfile, error) {
			return agent.UserProfile{
				Subject:           "物理",
				VisualPreferences: map[string]string{"color_scheme": "红色"},
			}, nil
		},
	}
	s := agent.NewTestSession(mock)
	req := agent.NewTaskRequirements(s.SessionID, s.UserID)
	req.TargetAudience = "高三学生"
	req.GlobalStyle = "绿色"
	s.PrefillFromMemory(req)

	if req.TargetAudience != "高三学生" {
		t.Errorf("should not overwrite existing audience, got %q", req.TargetAudience)
	}
	if req.GlobalStyle != "绿色" {
		t.Errorf("should not overwrite existing style, got %q", req.GlobalStyle)
	}
}
