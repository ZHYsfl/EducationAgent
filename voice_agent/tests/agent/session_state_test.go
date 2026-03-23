package agent_test

import (
	agent "voiceagent/agent"
	"sync"
	"testing"
)

// ===========================================================================
// SessionState String
// ===========================================================================

func TestSessionState_String(t *testing.T) {
	tests := []struct {
		state agent.SessionState
		want  string
	}{
		{agent.StateIdle, "idle"},
		{agent.StateListening, "listening"},
		{agent.StateProcessing, "processing"},
		{agent.StateSpeaking, "speaking"},
		{agent.SessionState(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("SessionState(%d).String() = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// State management
// ===========================================================================

func TestSession_GetSetState(t *testing.T) {
	s := agent.NewTestSession(nil)
	if s.GetState() != agent.StateIdle {
		t.Errorf("initial state should be Idle, got %s", s.GetState())
	}
	s.SetStateRaw(agent.StateListening) // bypass SendJSON for unit test
	if s.GetState() != agent.StateListening {
		t.Errorf("state should be Listening, got %s", s.GetState())
	}
}

func TestSession_SetState_SendsWSMessage(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.SetState(agent.StateProcessing)
	msgs := agent.DrainWriteCh(s)
	found, ok := agent.FindWSMessage(msgs, "status")
	if !ok {
		t.Fatal("expected status message")
	}
	if found.State != "processing" {
		t.Errorf("state = %q, want processing", found.State)
	}
}

func TestSession_StateConcurrency(t *testing.T) {
	s := agent.NewTestSession(nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				s.SetState(agent.StateListening)
			} else {
				_ = s.GetState()
			}
		}(i)
	}
	wg.Wait()
}

// ===========================================================================
// Task management
// ===========================================================================

func TestSession_RegisterAndOwnsTask(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.RegisterTask("task_001", "高等数学")
	if !s.OwnsTask("task_001") {
		t.Error("should own task_001")
	}
	if s.OwnsTask("task_999") {
		t.Error("should not own task_999")
	}
}

func TestSession_SetActiveTask(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.SetActiveTask("task_001")
	if s.GetActiveTask() != "task_001" {
		t.Errorf("active = %q, want task_001", s.GetActiveTask())
	}
}

func TestSession_GetAllTasks(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.RegisterTask("task_a", "数学")
	s.RegisterTask("task_b", "物理")
	tasks := s.GetAllTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

// ===========================================================================
// ResolveTaskID
// ===========================================================================

func TestResolveTaskID_ActiveTask(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.RegisterTask("task_001", "数学")
	s.RegisterTask("task_002", "物理")
	s.SetActiveTask("task_001")

	tid, ok := s.ResolveTaskID()
	if !ok || tid != "task_001" {
		t.Errorf("expected task_001, got %q ok=%v", tid, ok)
	}
}

func TestResolveTaskID_SingleTask(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.RegisterTask("task_only", "唯一课件")

	tid, ok := s.ResolveTaskID()
	if !ok || tid != "task_only" {
		t.Errorf("expected task_only, got %q ok=%v", tid, ok)
	}
}

func TestResolveTaskID_NoTasks(t *testing.T) {
	s := agent.NewTestSession(nil)
	tid, ok := s.ResolveTaskID()
	if ok || tid != "" {
		t.Errorf("no tasks: expected empty, got %q ok=%v", tid, ok)
	}
}

func TestResolveTaskID_MultipleTasks_NoActive(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.RegisterTask("task_a", "数学")
	s.RegisterTask("task_b", "物理")

	tid, ok := s.ResolveTaskID()
	if ok {
		t.Errorf("multiple tasks no active: should return resolved=false, got %q", tid)
	}
}

func TestResolveTaskID_ActiveNotOwned(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.RegisterTask("task_a", "数学")
	s.SetActiveTask("task_stale")

	tid, ok := s.ResolveTaskID()
	if !ok || tid != "task_a" {
		t.Errorf("stale active, single owned: expected task_a, got %q ok=%v", tid, ok)
	}
}
