package voiceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"memory_service/internal/service"
)

type Client struct {
	baseURL     string
	path        string
	internalKey string
	httpClient  *http.Client
	retryMax    int
}

func NewClient(baseURL, path, internalKey string, timeout time.Duration, retryMax int) *Client {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if retryMax <= 0 {
		retryMax = 1
	}
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/api/v1/voice/ppt_message"
	}
	return &Client{
		baseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		path:        path,
		internalKey: strings.TrimSpace(internalKey),
		httpClient:  &http.Client{Timeout: timeout},
		retryMax:    retryMax,
	}
}

type callbackEnvelope struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id,omitempty"`
	MsgType   string `json:"msg_type,omitempty"`
	Priority  string `json:"priority,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

func (c *Client) SendPPTMessage(ctx context.Context, req service.VoicePPTMessageRequest) error {
	if c.baseURL == "" {
		return fmt.Errorf("voice agent base url is required")
	}
	payload := callbackEnvelope{
		TaskID:    req.TaskID,
		SessionID: req.SessionID,
		RequestID: req.RequestID,
		MsgType:   req.MsgType,
		Priority:  req.Priority,
		Summary:   req.Summary,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := c.baseURL + c.path
	var lastErr error
	for attempt := 1; attempt <= c.retryMax; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.internalKey != "" {
			httpReq.Header.Set("X-Internal-Key", c.internalKey)
		}
		resp, err := c.httpClient.Do(httpReq)
		if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("callback status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		if attempt < c.retryMax {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("callback failed")
	}
	return lastErr
}
