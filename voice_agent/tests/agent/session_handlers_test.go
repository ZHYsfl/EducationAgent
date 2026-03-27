package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"testing"
	"time"
)

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

	if req.Subject != "数学" {
		t.Errorf("subject = %q, want 数学", req.Subject)
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
	req.Subject = "化学"
	req.GlobalStyle = "绿色"
	s.PrefillFromMemory(req)

	if req.Subject != "化学" {
		t.Errorf("should not overwrite existing subject, got %q", req.Subject)
	}
	if req.GlobalStyle != "绿色" {
		t.Errorf("should not overwrite existing style, got %q", req.GlobalStyle)
	}
}
