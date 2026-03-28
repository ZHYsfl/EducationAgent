package agent_test

import (
	agent "voiceagent/agent"
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
// Other tests
// ===========================================================================
