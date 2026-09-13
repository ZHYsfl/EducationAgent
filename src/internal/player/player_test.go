package player

import "testing"

func TestPlayerSentenceLifecycle(t *testing.T) {
	p := New()

	p.OnSentenceStart("第一句。")
	p.OnProgress(2)
	p.OnSentenceEnded()

	p.OnSentenceStart("第二句。")
	p.OnSentenceEnded()

	if got := p.DoneCount(); got != 2 {
		t.Fatalf("DoneCount = %d, want 2", got)
	}
}

func TestPlayerStopAndSnapshotTakesPartialCurrent(t *testing.T) {
	p := New()

	p.OnSentenceStart("已经播完的句子。")
	p.OnSentenceEnded()

	p.OnSentenceStart("播了一半的句子。")
	p.OnProgress(len([]rune("播了一半")))

	said := p.StopAndSnapshot()
	want := "已经播完的句子。" + "播了一半"
	if said != want {
		t.Fatalf("said = %q, want %q", said, want)
	}
	if !p.Stopped() {
		t.Fatal("stopped flag not set")
	}
	if got := p.DoneCount(); got != 0 {
		t.Fatalf("ledger not cleared after snapshot, DoneCount = %d", got)
	}
}

func TestPlayerProgressClampedToSentenceLength(t *testing.T) {
	p := New()
	p.OnSentenceStart("短句。")
	p.OnProgress(100)
	if p.pos != len([]rune("短句。")) {
		t.Fatalf("pos = %d, want clamped to %d", p.pos, len([]rune("短句。")))
	}
	p.OnSentenceEnded()
	if got := p.DoneCount(); got != 1 {
		t.Fatalf("DoneCount = %d, want 1", got)
	}
}

func TestPlayerProgressWithoutCurrentSentenceIgnored(t *testing.T) {
	p := New()
	p.OnProgress(5) // no cur: must not panic or record
	if p.pos != 0 {
		t.Fatalf("pos = %d, want 0", p.pos)
	}
}

func TestPlayerEndedWithoutCurrentSentenceIgnored(t *testing.T) {
	p := New()
	p.OnSentenceEnded()
	if got := p.DoneCount(); got != 0 {
		t.Fatalf("DoneCount = %d, want 0", got)
	}
}
