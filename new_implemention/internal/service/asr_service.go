package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ASRService transcribes audio into text.
type ASRService interface {
	Transcribe(ctx context.Context, audioBase64 string) (string, error)
}

// DefaultASRService calls an external ASR endpoint via an OpenAI-compatible HTTP API.
type DefaultASRService struct {
	client   *http.Client
	baseURL  string
	modelID  string
	apiKey   string
}

// NewASRService creates the real ASR client from environment variables.
func NewASRService() ASRService {
	baseURL := strings.TrimSpace(os.Getenv("ASR_OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = "http://localhost:6006/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	modelID := strings.TrimSpace(os.Getenv("ASR_MODEL_ID"))
	if modelID == "" {
		modelID = "/root/autodl-tmp/asr"
	}

	apiKey := strings.TrimSpace(os.Getenv("ASR_API_KEY"))
	if apiKey == "" {
		apiKey = "EMPTY"
	}

	return &DefaultASRService{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: baseURL,
		modelID: modelID,
		apiKey:  apiKey,
	}
}

// Transcribe sends the base64 audio payload to the ASR endpoint and returns the transcript.
func (s *DefaultASRService) Transcribe(ctx context.Context, audioBase64 string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// Limit to ~30s of 16kHz 16-bit mono audio to stay within ASR context limits.
	const maxBase64Len = 1_280_000
	if len(audioBase64) > maxBase64Len {
		audioBase64 = audioBase64[:maxBase64Len]
	}

	// Build OpenAI-compatible chat completion request with inline audio data URL.
	payload := map[string]any{
		"model": s.modelID,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "audio_url",
						"audio_url": map[string]any{
							"url": "data:audio/wav;base64," + audioBase64,
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal asr request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create asr request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("asr request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read asr response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asr service returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal asr response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("asr response has no choices")
	}

	return extractASRText(result.Choices[0].Message.Content), nil
}

func extractASRText(raw string) string {
	start := strings.Index(raw, "<asr_text>")
	end := strings.Index(raw, "</asr_text>")
	if start >= 0 && end > start {
		return strings.TrimSpace(raw[start+10 : end])
	}
	if idx := strings.Index(raw, "<asr_text>"); idx >= 0 {
		return strings.TrimSpace(raw[idx+10:])
	}
	return strings.TrimSpace(raw)
}

// StubASRService is a non-functional placeholder for tests.
type StubASRService struct {
	Override func(audioBase64 string) string
}

func NewStubASRService() ASRService {
	return &StubASRService{}
}

func (s *StubASRService) Transcribe(ctx context.Context, audioBase64 string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.Override != nil {
		return s.Override(audioBase64), nil
	}
	return fmt.Sprintf("transcript_%d", len(audioBase64)), nil
}
