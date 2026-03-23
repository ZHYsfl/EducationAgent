package audio

import (
	"testing"
)

func TestAudioBuffer_WriteAndGetBlock(t *testing.T) {
	ab := NewAudioBuffer()

	data := make([]byte, BlockSize)
	for i := range data {
		data[i] = byte(i % 256)
	}
	ab.Write(data)

	block, ok := ab.GetBlock()
	if !ok {
		t.Fatal("expected to get a block")
	}
	if len(block) != BlockSize {
		t.Fatalf("block size = %d, want %d", len(block), BlockSize)
	}

	// No more blocks
	_, ok = ab.GetBlock()
	if ok {
		t.Error("should not have another block")
	}
}

func TestAudioBuffer_Flush(t *testing.T) {
	ab := NewAudioBuffer()
	ab.Write([]byte{1, 2, 3})
	flushed := ab.Flush()
	if len(flushed) != 3 {
		t.Errorf("flush len = %d, want 3", len(flushed))
	}
	if ab.Len() != 0 {
		t.Error("buffer should be empty after flush")
	}
}

func TestAudioBuffer_Reset(t *testing.T) {
	ab := NewAudioBuffer()
	ab.Write([]byte{1, 2, 3})
	ab.Reset()
	if ab.Len() != 0 {
		t.Error("buffer should be empty after reset")
	}
}

func TestAudioBuffer_FlushEmpty(t *testing.T) {
	ab := NewAudioBuffer()
	if flushed := ab.Flush(); flushed != nil {
		t.Errorf("flush of empty buffer should be nil, got %v", flushed)
	}
}

func TestAudioBuffer_PartialThenBlock(t *testing.T) {
	ab := NewAudioBuffer()
	half := BlockSize / 2
	ab.Write(make([]byte, half))
	_, ok := ab.GetBlock()
	if ok {
		t.Error("should not have a full block yet")
	}
	ab.Write(make([]byte, half))
	_, ok = ab.GetBlock()
	if !ok {
		t.Error("should have a full block now")
	}
}
