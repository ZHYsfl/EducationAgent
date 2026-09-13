package voiceengine

import "testing"

func TestToolInfoAddGetReset(t *testing.T) {
	var ti ToolInfo

	if got := ti.Get(); len(got) != 0 {
		t.Fatalf("fresh ToolInfo = %v, want empty", got)
	}

	ti.Add(
		ToolCallRecord{Name: "remember", ArgsJSON: `{"k":"v"}`, Result: "ok"},
		ToolCallRecord{Name: "ask", ArgsJSON: `{}`, Result: "42"},
	)

	got := ti.Get()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "remember" || got[1].Name != "ask" {
		t.Fatalf("order not preserved: %+v", got)
	}

	// Get hands out a copy: mutating it must not corrupt the internal state.
	got[0].Result = "tampered"
	if ti.Get()[0].Result != "ok" {
		t.Fatal("Get must return a copy")
	}

	ti.Reset()
	if got := ti.Get(); len(got) != 0 {
		t.Fatalf("after Reset = %v, want empty", got)
	}
}
