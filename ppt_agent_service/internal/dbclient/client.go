package dbclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Base   string
	Key    string
	Client *http.Client
}

func New(baseURL, internalKey string, timeoutSec int) *Client {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &Client{
		Base: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Key:  strings.TrimSpace(internalKey),
		Client: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
	}
}

func (c *Client) headers() http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	if c.Key != "" {
		h.Set("X-Internal-Key", c.Key)
	}
	return h
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header = c.headers()
	return c.Client.Do(req)
}

// EnsureUserSession calls ensure-user + ensure-session.
func (c *Client) EnsureUserSession(ctx context.Context, userID, sessionID string) error {
	if c == nil || c.Base == "" {
		return nil
	}
	resp, err := c.doJSON(ctx, http.MethodPost, "/internal/db/ensure-user", map[string]string{
		"user_id": userID, "display_name": "",
	})
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return err
	}
	if resp != nil && resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ensure-user: %s: %s", resp.Status, string(b))
	}
	resp2, err := c.doJSON(ctx, http.MethodPost, "/internal/db/ensure-session", map[string]string{
		"session_id": sessionID, "user_id": userID, "title": "",
	})
	if resp2 != nil {
		defer resp2.Body.Close()
		if err == nil && resp2.StatusCode >= 300 {
			b, _ := io.ReadAll(resp2.Body)
			return fmt.Errorf("ensure-session: %s: %s", resp2.Status, string(b))
		}
	}
	return err
}

func (c *Client) SaveTask(ctx context.Context, body map[string]any) error {
	resp, err := c.doJSON(ctx, http.MethodPut, "/internal/db/tasks/"+fmt.Sprint(body["id"]), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("save_task: %s: %s", resp.Status, string(b))
	}
	return nil
}

func (c *Client) LoadTask(ctx context.Context, taskID string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/internal/db/tasks/"+taskID, nil)
	req.Header = c.headers()
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("load_task: %s", string(b))
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *Client) ListTasksLight(ctx context.Context, sessionID string, page, pageSize int, userID string) ([]map[string]any, int, error) {
	ub, err := url.Parse(c.Base + "/internal/db/task-list")
	if err != nil {
		return nil, 0, err
	}
	q := ub.Query()
	q.Set("session_id", sessionID)
	q.Set("page", strconv.Itoa(page))
	q.Set("page_size", strconv.Itoa(pageSize))
	if userID != "" {
		q.Set("user_id", userID)
	}
	ub.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ub.String(), nil)
	req.Header = c.headers()
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, 0, fmt.Errorf("list: %s", string(b))
	}
	var data struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, 0, err
	}
	return data.Items, data.Total, nil
}

func (c *Client) SaveExport(ctx context.Context, body map[string]any) error {
	eid := fmt.Sprint(body["export_id"])
	resp, err := c.doJSON(ctx, http.MethodPut, "/internal/db/exports/"+eid, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("save_export: %s", string(b))
	}
	return nil
}

func (c *Client) LoadExport(ctx context.Context, exportID string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/internal/db/exports/"+exportID, nil)
	req.Header = c.headers()
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("load_export http %d", resp.StatusCode)
	}
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return m, nil
}
