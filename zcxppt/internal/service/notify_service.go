package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type NotifyService struct {
	voiceAgentURL string
	httpClient    *http.Client
}

func NewNotifyService(voiceAgentURL string) *NotifyService {
	return &NotifyService{
		voiceAgentURL: strings.TrimRight(voiceAgentURL, "/"),
		httpClient:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *NotifyService) SendPPTMessage(ctx context.Context, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := s.voiceAgentURL + "/api/v1/voice/ppt_message"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("voice notify failed with status %d", resp.StatusCode)
	}
	return nil
}
