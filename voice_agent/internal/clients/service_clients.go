package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	cfg "voiceagent/internal/config"
	types "voiceagent/internal/types"
)

type ServiceClients struct {
	httpClient *http.Client

	pptBaseURL    string
	kbBaseURL     string
	memoryBaseURL string
	searchBaseURL string
	dbBaseURL     string
}

func NewServiceClients(config *cfg.Config) *ServiceClients {
	return &ServiceClients{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		pptBaseURL:    strings.TrimRight(config.PPTAgentURL, "/"),
		kbBaseURL:     strings.TrimRight(config.KBServiceURL, "/"),
		memoryBaseURL: strings.TrimRight(config.MemoryURL, "/"),
		searchBaseURL: strings.TrimRight(config.SearchURL, "/"),
		dbBaseURL:     strings.TrimRight(config.DBServiceURL, "/"),
	}
}

// PostJSON and GetJSON expose the internal HTTP helpers for black-box tests.
func (c *ServiceClients) PostJSON(ctx context.Context, url string, body any, out any) error {
	return c.postJSON(ctx, url, body, out)
}

// GetJSON sends GET and decodes JSON (exported for tests).
func (c *ServiceClients) GetJSON(ctx context.Context, url string, out any) error {
	return c.getJSON(ctx, url, out)
}

// BaseURLs returns trimmed service base URLs (for tests).
func (c *ServiceClients) BaseURLs() (ppt, kb, memory, search, db string) {
	return c.pptBaseURL, c.kbBaseURL, c.memoryBaseURL, c.searchBaseURL, c.dbBaseURL
}

func (c *ServiceClients) QueryKB(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error) {
	var out types.KBQueryResponse
	err := c.postJSON(ctx, c.kbBaseURL+"/api/v1/kb/query", req, &out)
	return out, err
}

func (c *ServiceClients) RecallMemory(ctx context.Context, req types.MemoryRecallRequest) (types.MemoryRecallResponse, error) {
	var out types.MemoryRecallResponse
	err := c.postJSON(ctx, c.memoryBaseURL+"/api/v1/memory/recall", req, &out)
	return out, err
}

func (c *ServiceClients) GetUserProfile(ctx context.Context, userID string) (types.UserProfile, error) {
	var out types.UserProfile
	err := c.getJSON(ctx, c.memoryBaseURL+"/api/v1/memory/profile/"+url.PathEscape(userID), &out)
	return out, err
}

func (c *ServiceClients) SearchWeb(ctx context.Context, req types.SearchRequest) (types.SearchResponse, error) {
	var out types.SearchResponse
	err := c.postJSON(ctx, c.searchBaseURL+"/api/v1/search/query", req, &out)
	return out, err
}

func (c *ServiceClients) GetSearchResults(ctx context.Context, requestID string) (types.SearchResponse, error) {
	var out types.SearchResponse
	endpoint := c.searchBaseURL + "/api/v1/search/results/" + url.QueryEscape(requestID)
	err := c.getJSON(ctx, endpoint, &out)
	return out, err
}

func (c *ServiceClients) InitPPT(ctx context.Context, req types.PPTInitRequest) (types.PPTInitResponse, error) {
	var out types.PPTInitResponse
	err := c.postJSON(ctx, c.pptBaseURL+"/api/v1/ppt/init", req, &out)
	return out, err
}

func (c *ServiceClients) SendFeedback(ctx context.Context, req types.PPTFeedbackRequest) error {
	return c.postJSON(ctx, c.pptBaseURL+"/api/v1/ppt/feedback", req, nil)
}

func (c *ServiceClients) GetCanvasStatus(ctx context.Context, taskID string) (types.CanvasStatusResponse, error) {
	var out types.CanvasStatusResponse
	endpoint := c.pptBaseURL + "/api/v1/canvas/status?task_id=" + url.QueryEscape(taskID)
	err := c.getJSON(ctx, endpoint, &out)
	return out, err
}

func (c *ServiceClients) UploadFile(r *http.Request) (json.RawMessage, error) {
	if r.Body == nil {
		return nil, fmt.Errorf("request body is empty")
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, c.dbBaseURL+"/api/v1/files/upload", r.Body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("db upload status=%d body=%s", resp.StatusCode, string(body))
	}
	var wrapped types.APIResponse
	if err := json.Unmarshal(body, &wrapped); err == nil && len(wrapped.Data) > 0 {
		if wrapped.Code != 0 && wrapped.Code != 200 {
			return nil, fmt.Errorf("db upload error code=%d msg=%s", wrapped.Code, wrapped.Message)
		}
		return wrapped.Data, nil
	}
	return json.RawMessage(body), nil
}

func (c *ServiceClients) ExtractMemory(ctx context.Context, req types.MemoryExtractRequest) (types.MemoryExtractResponse, error) {
	var out types.MemoryExtractResponse
	err := c.postJSON(ctx, c.memoryBaseURL+"/api/v1/memory/extract", req, &out)
	return out, err
}

func (c *ServiceClients) SaveWorkingMemory(ctx context.Context, req types.WorkingMemorySaveRequest) error {
	return c.postJSON(ctx, c.memoryBaseURL+"/api/v1/memory/working/save", req, nil)
}

func (c *ServiceClients) GetWorkingMemory(ctx context.Context, sessionID string) (*types.WorkingMemory, error) {
	var out types.WorkingMemory
	err := c.getJSON(ctx, c.memoryBaseURL+"/api/v1/memory/working/"+url.PathEscape(sessionID), &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *ServiceClients) NotifyVADEvent(ctx context.Context, event types.VADEvent) error {
	return c.postJSON(ctx, c.pptBaseURL+"/api/v1/canvas/vad-event", event, nil)
}

func (c *ServiceClients) IngestFromSearch(ctx context.Context, req types.IngestFromSearchRequest) error {
	return c.postJSON(ctx, c.kbBaseURL+"/api/v1/kb/ingest-from-search", req, nil)
}

func (c *ServiceClients) postJSON(ctx context.Context, endpoint string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("post %s status=%d body=%s", endpoint, resp.StatusCode, string(respBody))
	}
	return DecodeAPIData(respBody, out)
}

func (c *ServiceClients) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("get %s status=%d body=%s", endpoint, resp.StatusCode, string(respBody))
	}
	return DecodeAPIData(respBody, out)
}

// DecodeAPIData unwraps an APIResponse envelope if present, then JSON-decodes
// the inner data into out. If out is nil, it is a no-op.
func DecodeAPIData(raw []byte, out any) error {
	if out == nil {
		return nil
	}

	var wrapped types.APIResponse
	if err := json.Unmarshal(raw, &wrapped); err == nil && (wrapped.Code != 0 || len(wrapped.Data) > 0) {
		if wrapped.Code != 0 && wrapped.Code != 200 {
			return fmt.Errorf("api error code=%d msg=%s", wrapped.Code, wrapped.Message)
		}
		if len(wrapped.Data) == 0 || string(wrapped.Data) == "null" {
			return nil
		}
		return json.Unmarshal(wrapped.Data, out)
	}
	return json.Unmarshal(raw, out)
}
