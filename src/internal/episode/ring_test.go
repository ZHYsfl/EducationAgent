package episode

import (
	"bytes"
	"testing"
)

func TestRingWrapAndPrefix(t *testing.T) {
	r := NewRing(100)

	a := bytes.Repeat([]byte{0xA1}, 60)
	b := bytes.Repeat([]byte{0xB2}, 60)
	r.Write(a)
	r.Mark()
	r.Write(b)

	got := r.Prefix()
	if len(got) != 40 || !bytes.Equal(got, a[20:]) {
		t.Fatalf("prefix = %d bytes, want the 40 bytes before the mark", len(got))
	}

	r.Write(bytes.Repeat([]byte{0xC3}, 30))
	// Eviction slides the mark region itself: only its retained tail survives.
	if again := r.Prefix(); !bytes.Equal(again, a[50:]) {
		t.Fatalf("prefix after eviction = %d bytes, want retained mark tail %d bytes", len(again), len(a[50:]))
	}
}

func TestRingMarkAtCurrentEnd(t *testing.T) {
	r := NewRing(10)
	r.Write([]byte{1, 2, 3})
	r.Mark()
	r.Write([]byte{4, 5})
	if got := r.Prefix(); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("prefix = %v", got)
	}
}
