package service

import (
	"context"
	"fmt"
)

// ASRService transcribes audio into text.
type ASRService interface {
	Transcribe(ctx context.Context, audioBase64 string) (string, error)
}

// StubASRService is a non-functional placeholder for the MVP.
// In production this would call a local Whisper model or similar.
type StubASRService struct {
	// Override allows tests to inject arbitrary transcripts.
	Override func(audioBase64 string) string
}

// NewASRService creates the default ASR stub.
func NewASRService() ASRService {
	return &StubASRService{}
}

// Transcribe returns a dummy transcript so the handler layer can be tested
// without a real model. If Override is set, its result is used instead.
func (s *StubASRService) Transcribe(ctx context.Context, audioBase64 string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Override != nil {
		return s.Override(audioBase64), nil
	}
	// Return a deterministic placeholder based on payload size.
	return fmt.Sprintf("transcript_%d", len(audioBase64)), nil
}
