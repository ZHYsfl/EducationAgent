package state

import "testing"

func TestUpdateRequirementsAndMissingFields(t *testing.T) {
	s := NewAppState()
	missing, err := s.UpdateRequirements(map[string]any{
		"topic": "AI",
		"style": "modern",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"total_pages", "audience"}
	if len(missing) != len(want) {
		t.Fatalf("missing fields = %v, want %v", missing, want)
	}
	for i, f := range want {
		if missing[i] != f {
			t.Fatalf("missing[%d] = %q, want %q", i, missing[i], f)
		}
	}
}

func TestUpdateRequirementsAfterFinalized(t *testing.T) {
	s := NewAppState()
	s.MarkRequirementsFinalized()
	if _, err := s.UpdateRequirements(map[string]any{"topic": "x"}); err == nil {
		t.Fatal("expected error after finalized")
	}
}

func TestRequireConfirmIncomplete(t *testing.T) {
	s := NewAppState()
	if err := s.RequireConfirm(); err == nil {
		t.Fatal("expected error for incomplete requirements")
	}
}

func TestRequireConfirmComplete(t *testing.T) {
	s := NewAppState()
	tp := "topic"
	st := "style"
	n := 10
	a := "audience"
	_, _ = s.UpdateRequirements(map[string]any{
		"topic":       tp,
		"style":       st,
		"total_pages": n,
		"audience":    a,
	})
	if err := s.RequireConfirm(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPPTToVoiceQueue(t *testing.T) {
	s := NewAppState()
	s.SendToVoiceAgent("hello")
	s.SendToVoiceAgent("world")
	got := s.DrainPPTToVoiceQueue()
	want := []string{"hello", "world"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], v)
		}
	}
	got2 := s.DrainPPTToVoiceQueue()
	if len(got2) != 0 {
		t.Fatalf("expected empty queue, got %v", got2)
	}
}

func TestVoiceToPPTQueue(t *testing.T) {
	s := NewAppState()
	s.SendToPPTAgent("a")
	s.SendToPPTAgent("b")
	got := s.DrainVoiceToPPTQueue()
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], v)
		}
	}
	if s.VoiceToPPTQueueLen() != 0 {
		t.Fatalf("expected empty queue, len=%d", s.VoiceToPPTQueueLen())
	}
}

func TestResetConversation(t *testing.T) {
	s := NewAppState()
	_, _ = s.UpdateRequirements(map[string]any{"topic": "x"})
	s.SendToPPTAgent("data")
	s.ResetConversation()
	if s.GetRequirements().Topic != nil {
		t.Fatal("requirements should be reset")
	}
	if s.VoiceToPPTQueueLen() != 0 {
		t.Fatal("voice queue should be empty")
	}
}
