package agent_test

import (
	agent "voiceagent/agent"
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
	mock := &agent.MockServices{
		InitPPTFn: func(ctx context.Context, req agent.PPTInitRequest) (agent.PPTInitResponse, error) {
			return agent.PPTInitResponse{TaskID: "task_created_001"}, nil
		},
	}
	s := agent.NewTestSession(mock)
	_ = agent.NewTestPipelineWithTTS(s, mock)
	req := makeFullRequirements()
	req.SessionID = s.SessionID
	req.UserID = s.UserID
	s.SetRequirements(req)

	agent.RegisterSession(s)
	defer agent.UnregisterSession(s)

	s.CreatePPTFromRequirements()

	if !s.OwnsTask("task_created_001") {
		t.Error("should own the created task")
	}
	if s.GetActiveTask() != "task_created_001" {
		t.Error("should set active task")
	}

	msgs := agent.DrainWriteCh(s)
	found, ok := agent.FindWSMessage(msgs, "task_created")
	if !ok {
		t.Fatal("expected task_created message")
	}
	if found.TaskID != "task_created_001" {
		t.Error("task_id mismatch in WS message")
	}

	s.RLockReqMu()
	if s.GetRequirements().Status != "generating" {
		t.Errorf("status = %q, want generating", s.GetRequirements().Status)
	}
	s.RUnlockReqMu()
}

// ===========================================================================
// onVADStart state transitions
// ===========================================================================

func TestOnVADStart_Idle_TransitionsToListening(t *testing.T) {
	// We can't fully test onVADStart because it starts goroutines that
	// require ASR/LLM. But we can verify publishVADEvent is called.
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	s.SetActiveTask("task_1")

	// Manually call publishVADEvent to verify it works
	s.PublishVADEvent()
	time.Sleep(30 * time.Millisecond)

	mock.Mu.Lock()
	callCount := len(mock.VADEventCalls)
	mock.Mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 VAD event, got %d", callCount)
	}
}

// ===========================================================================
// handleAudioData
// ===========================================================================

func TestHandleAudioData_IgnoresWhenNotListening(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.SetStateRaw(agent.StateIdle)
	s.HandleAudioData([]byte{1, 2, 3})
	// should not panic, audio is ignored
}

// ===========================================================================
// WSMessage JSON serialization
// ===========================================================================

func TestWSMessage_JSON_Roundtrip(t *testing.T) {
	msg := agent.WSMessage{
		Type:   "task_created",
		TaskID: "task_json",
		Topic:  "测试",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded agent.WSMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "task_created" || decoded.TaskID != "task_json" {
		t.Error("roundtrip failed")
	}
}

func TestWSMessage_JSON_OmitsEmpty(t *testing.T) {
	msg := agent.WSMessage{Type: "status", State: "idle"}
	data, _ := json.Marshal(msg)
	s := string(data)
	if strings.Contains(s, "task_id") {
		t.Error("should omit empty task_id")
	}
	if strings.Contains(s, "page_id") {
		t.Error("should omit empty page_id")
	}
}
