package main

import (
	"bytes"
	"sync"
)

const (
	SampleRate     = 16000                                       // 采样率 16kHz，与麦克风采集一致
	BytesPerSample = 2                                           // 每采样 2 字节（Int16）
	BlockDuration  = 1                                           // 每块 1 秒
	BlockSize      = SampleRate * BytesPerSample * BlockDuration // 32000 字节 = 1 秒
)

type AudioBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func NewAudioBuffer() *AudioBuffer {
	return &AudioBuffer{}
}

func (ab *AudioBuffer) Write(data []byte) {
	ab.mu.Lock()
	ab.buf.Write(data)
	ab.mu.Unlock()
}

// GetBlock returns a complete 1-second audio block if available.
func (ab *AudioBuffer) GetBlock() ([]byte, bool) {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	if ab.buf.Len() < BlockSize {
		return nil, false
	}

	block := make([]byte, BlockSize)
	ab.buf.Read(block)
	return block, true
}

// Flush returns all remaining audio data (possibly less than 1s).
func (ab *AudioBuffer) Flush() []byte {
	ab.mu.Lock()
	defer ab.mu.Unlock()

	if ab.buf.Len() == 0 {
		return nil
	}
	data := make([]byte, ab.buf.Len())
	ab.buf.Read(data)
	return data
}

func (ab *AudioBuffer) Reset() {
	ab.mu.Lock()
	ab.buf.Reset()
	ab.mu.Unlock()
}

func (ab *AudioBuffer) Len() int {
	ab.mu.Lock()
	defer ab.mu.Unlock()
	return ab.buf.Len()
}
