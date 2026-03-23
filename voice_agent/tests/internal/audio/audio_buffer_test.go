package audio_test

import (
	"testing"

	"voiceagent/internal/audio"
)

func TestAudioBuffer_WriteAndGetBlock(t *testing.T) {
	ab := audio.NewAudioBuffer()

	data := make([]byte, audio.BlockSize)
	for i := range data {
		data[i] = byte(i % 256)
	}
	ab.Write(data)

	block, ok := ab.GetBlock()
	if !ok {
		t.Fatal("expected to get a block")
	}
	if len(block) != audio.BlockSize {
		t.Fatalf("block size = %d, want %d", len(block), audio.BlockSize)
	}

	_, ok = ab.GetBlock()
	if ok {
		t.Error("should not have another block")
	}
}

func TestAudioBuffer_Flush(t *testing.T) {
	ab := audio.NewAudioBuffer()
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
	ab := audio.NewAudioBuffer()
	ab.Write([]byte{1, 2, 3})
	ab.Reset()
	if ab.Len() != 0 {
		t.Error("buffer should be empty after reset")
	}
}

func TestAudioBuffer_FlushEmpty(t *testing.T) {
	ab := audio.NewAudioBuffer()
	if flushed := ab.Flush(); flushed != nil {
		t.Errorf("flush of empty buffer should be nil, got %v", flushed)
	}
}

func TestAudioBuffer_PartialThenBlock(t *testing.T) {
	ab := audio.NewAudioBuffer()
	half := audio.BlockSize / 2
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
