// Package tools holds clients for external tool backends.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ArmGateway is the HTTP client for the embodied-tool RESTful gateway
// (api_of_embodied_tools.md, default http://127.0.0.1:8000). It covers the
// four physical tools; the two cross-agent communication tools
// (send_to_voice_agent / get_message_from_voice_agent) operate on the
// orchestration-held queues directly and do NOT go through this client.
type ArmGateway struct {
	baseURL string
	client  *http.Client
}

// NewArmGateway creates a gateway client. baseURL may carry or omit a trailing slash.
func NewArmGateway(baseURL string) *ArmGateway {
	return &ArmGateway{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		client:  &http.Client{Timeout: 10 * time.Minute}, // physical moves can take long; calls are blocking by contract
	}
}

// gatewayResponse mirrors the uniform envelope of the tool gateway:
// {code, result, error}. code==0 means the gateway call succeeded; the
// business outcome is carried by the tool's own result string.
type gatewayResponse struct {
	Code   int     `json:"code"`
	Result *string `json:"result"`
	Error  *string `json:"error"`
}

// call performs one gateway request and returns the tool's raw result string,
// passed through verbatim (training data must match it word for word).
func (g *ArmGateway) call(ctx context.Context, method, path string, body any) (string, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return "", fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, reader)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gateway unreachable: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	var env gatewayResponse
	if err := json.Unmarshal(data, &env); err != nil {
		return "", fmt.Errorf("invalid gateway response (HTTP %d): %s", resp.StatusCode, string(data))
	}
	if env.Code != 0 {
		errMsg := fmt.Sprintf("gateway code=%d", env.Code)
		if env.Error != nil {
			errMsg = *env.Error
		}
		return "", fmt.Errorf("%s", errMsg)
	}
	if env.Result == nil {
		return "", fmt.Errorf("gateway returned null result")
	}
	return *env.Result, nil
}

// GetCurrentCoordinates calls GET /api/v1/get_current_coordinates.
func (g *ArmGateway) GetCurrentCoordinates(ctx context.Context) (string, error) {
	return g.call(ctx, http.MethodGet, "/api/v1/get_current_coordinates", nil)
}

// MoveToCoordinates calls POST /api/v1/move_to_coordinates with {x,y,z}.
func (g *ArmGateway) MoveToCoordinates(ctx context.Context, x, y, z string) (string, error) {
	return g.call(ctx, http.MethodPost, "/api/v1/move_to_coordinates", map[string]string{
		"x": x, "y": y, "z": z,
	})
}

// GrabTheBlock calls POST /api/v1/grab_the_block with {color}.
func (g *ArmGateway) GrabTheBlock(ctx context.Context, color string) (string, error) {
	return g.call(ctx, http.MethodPost, "/api/v1/grab_the_block", map[string]string{
		"color": color,
	})
}

// ReleaseTheBlock calls POST /api/v1/release_the_block.
func (g *ArmGateway) ReleaseTheBlock(ctx context.Context) (string, error) {
	return g.call(ctx, http.MethodPost, "/api/v1/release_the_block", nil)
}
