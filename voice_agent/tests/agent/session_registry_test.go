package agent_test

import (
	agent "voiceagent/agent"
	"testing"
)

// ===========================================================================
// Session registry (package-level)
// ===========================================================================

func TestSessionRegistry(t *testing.T) {
	s1 := agent.NewTestSession(nil)
	s1.SessionID = "sess_reg_1"
	s1.RegisterTask("task_r1", "测试1")

	s2 := agent.NewTestSession(nil)
	s2.SessionID = "sess_reg_2"
	s2.RegisterTask("task_r2", "测试2")

	agent.RegisterSession(s1)
	agent.RegisterSession(s2)
	agent.RegisterTask("task_r1", s1.SessionID)
	agent.RegisterTask("task_r2", s2.SessionID)
	defer func() {
		agent.UnregisterSession(s1)
		agent.UnregisterSession(s2)
	}()

	found := agent.FindSessionByTaskID("task_r1")
	if found == nil || found.SessionID != "sess_reg_1" {
		t.Error("agent.FindSessionByTaskID(task_r1) failed")
	}

	found = agent.FindSessionByTaskID("task_r2")
	if found == nil || found.SessionID != "sess_reg_2" {
		t.Error("agent.FindSessionByTaskID(task_r2) failed")
	}

	found = agent.FindSessionByTaskID("task_nonexistent")
	if found != nil {
		t.Error("should return nil for nonexistent task")
	}
}

func TestUnregisterSession_CleansTaskIndex(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.SessionID = "sess_unreg"
	s.RegisterTask("task_u1", "测试")

	agent.RegisterSession(s)
	agent.RegisterTask("task_u1", s.SessionID)

	found := agent.FindSessionByTaskID("task_u1")
	if found == nil {
		t.Fatal("should find session before unregister")
	}

	agent.UnregisterSession(s)
	found = agent.FindSessionByTaskID("task_u1")
	if found != nil {
		t.Error("should not find session after unregister")
	}
}

// ===========================================================================
// SendJSON / SendAudio
// ===========================================================================

func TestSession_SendJSON(t *testing.T) {
	s := agent.NewTestSession(nil)
	agent.NewBarePipelineForTest(s)
	s.SendJSON(agent.WSMessage{Type: "test", Text: "hello"})

	msgs := agent.DrainWriteCh(s)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "test" || msgs[0].Text != "hello" {
		t.Error("message content mismatch")
	}
}

func TestSession_SendAudio(t *testing.T) {
	s := agent.NewTestSession(nil)
	agent.NewBarePipelineForTest(s)
	s.SendAudio([]byte{1, 2, 3})

	mt, _, ok := agent.DrainNextWriteItem(s)
	if !ok {
		t.Fatal("expected audio in writeCh")
	}
	if mt != 2 { // websocket.BinaryMessage
		t.Errorf("expected binary message type, got %d", mt)
	}
}
