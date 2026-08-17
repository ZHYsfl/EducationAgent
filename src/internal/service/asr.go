package service

import "context"

// ASRService transcribes audio to text.
type ASRService interface {
	// Transcribe converts the supplied audio (base64-encoded) to text.
	// Format is the audio container (e.g. "pcm").
	Transcribe(ctx context.Context, audioBase64 string, format string) (string, error)
}

// StubASR is a placeholder ASR implementation. It should be replaced with
// real inference using Qwen3-ASR-0.6B.
type StubASR struct{}

// Transcribe returns a placeholder string.
func (s *StubASR) Transcribe(ctx context.Context, audioBase64 string, format string) (string, error) {
	return "[ASR stub] implement real qwen3-asr-0.6b inference", nil
}
