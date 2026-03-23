package agent

import (
	"testing"

	adaptivepkg "voiceagent/internal/adaptive"
)

// ===========================================================================
// Session registry (package-level)
// ===========================================================================

func TestSessionRegistry(t *testing.T) {
	s1 := newTestSession(nil)
	s1.SessionID = "sess_reg_1"
	s1.RegisterTask("task_r1", "测试1")

	s2 := newTestSession(nil)
	s2.SessionID = "sess_reg_2"
	s2.RegisterTask("task_r2", "测试2")

	registerSession(s1)
	registerSession(s2)
	registerTask("task_r1", s1.SessionID)
	registerTask("task_r2", s2.SessionID)
	defer func() {
		unregisterSession(s1)
		unregisterSession(s2)
	}()

	found := findSessionByTaskID("task_r1")
	if found == nil || found.SessionID != "sess_reg_1" {
		t.Error("findSessionByTaskID(task_r1) failed")
	}

	found = findSessionByTaskID("task_r2")
	if found == nil || found.SessionID != "sess_reg_2" {
		t.Error("findSessionByTaskID(task_r2) failed")
	}

	found = findSessionByTaskID("task_nonexistent")
	if found != nil {
		t.Error("should return nil for nonexistent task")
	}
}

func TestUnregisterSession_CleansTaskIndex(t *testing.T) {
	s := newTestSession(nil)
	s.SessionID = "sess_unreg"
	s.RegisterTask("task_u1", "测试")

	registerSession(s)
	registerTask("task_u1", s.SessionID)

	found := findSessionByTaskID("task_u1")
	if found == nil {
		t.Fatal("should find session before unregister")
	}

	unregisterSession(s)
	found = findSessionByTaskID("task_u1")
	if found != nil {
		t.Error("should not find session after unregister")
	}
}

// ===========================================================================
// SendJSON / SendAudio
// ===========================================================================

func TestSession_SendJSON(t *testing.T) {
	s := newTestSession(nil)
	s.pipeline = &Pipeline{
		adaptive: adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes()),
	}
	s.SendJSON(WSMessage{Type: "test", Text: "hello"})

	msgs := drainWriteCh(s)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "test" || msgs[0].Text != "hello" {
		t.Error("message content mismatch")
	}
}

func TestSession_SendAudio(t *testing.T) {
	s := newTestSession(nil)
	s.pipeline = &Pipeline{
		adaptive: adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes()),
	}
	s.SendAudio([]byte{1, 2, 3})

	select {
	case item := <-s.writeCh:
		if item.msgType != 2 { // BinaryMessage
			t.Errorf("expected binary message type, got %d", item.msgType)
		}
	default:
		t.Error("expected audio in writeCh")
	}
}
