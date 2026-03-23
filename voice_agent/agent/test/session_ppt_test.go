package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// createPPTFromRequirements
// ===========================================================================

func TestCreatePPTFromRequirements(t *testing.T) {
	mock := &mockServices{
		InitPPTFn: func(ctx context.Context, req PPTInitRequest) (PPTInitResponse, error) {
			return PPTInitResponse{TaskID: "task_created_001"}, nil
		},
	}
	s := newTestSession(mock)
	_ = newTestPipelineWithTTS(s, mock)
	req := makeFullRequirements()
	req.SessionID = s.SessionID
	req.UserID = s.UserID
	s.Requirements = req

	registerSession(s)
	defer unregisterSession(s)

	s.createPPTFromRequirements()

	if !s.OwnsTask("task_created_001") {
		t.Error("should own the created task")
	}
	if s.GetActiveTask() != "task_created_001" {
		t.Error("should set active task")
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "task_created")
	if !ok {
		t.Fatal("expected task_created message")
	}
	if found.TaskID != "task_created_001" {
		t.Error("task_id mismatch in WS message")
	}

	s.reqMu.RLock()
	if s.Requirements.Status != "generating" {
		t.Errorf("status = %q, want generating", s.Requirements.Status)
	}
	s.reqMu.RUnlock()
}

// ===========================================================================
// onVADStart state transitions
// ===========================================================================

func TestOnVADStart_Idle_TransitionsToListening(t *testing.T) {
	// We can't fully test onVADStart because it starts goroutines that
	// require ASR/LLM. But we can verify publishVADEvent is called.
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SetActiveTask("task_1")

	// Manually call publishVADEvent to verify it works
	s.publishVADEvent()
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	callCount := len(mock.vadEventCalls)
	mock.mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 VAD event, got %d", callCount)
	}
}

// ===========================================================================
// handleAudioData
// ===========================================================================

func TestHandleAudioData_IgnoresWhenNotListening(t *testing.T) {
	s := newTestSession(nil)
	s.state = StateIdle
	s.handleAudioData([]byte{1, 2, 3})
	// should not panic, audio is ignored
}

// ===========================================================================
// WSMessage JSON serialization
// ===========================================================================

func TestWSMessage_JSON_Roundtrip(t *testing.T) {
	msg := WSMessage{
		Type:   "task_created",
		TaskID: "task_json",
		Topic:  "测试",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded WSMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "task_created" || decoded.TaskID != "task_json" {
		t.Error("roundtrip failed")
	}
}

func TestWSMessage_JSON_OmitsEmpty(t *testing.T) {
	msg := WSMessage{Type: "status", State: "idle"}
	data, _ := json.Marshal(msg)
	s := string(data)
	if strings.Contains(s, "task_id") {
		t.Error("should omit empty task_id")
	}
	if strings.Contains(s, "page_id") {
		t.Error("should omit empty page_id")
	}
}
