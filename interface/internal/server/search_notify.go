package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type voicePPTMessage struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	EventType string `json:"event_type"`
	TTSText   string `json:"tts_text"`
	Priority  string `json:"priority,omitempty"`
	ErrorCode int    `json:"error_code,omitempty"`
}

type kbIngestBody struct {
	SessionID    string         `json:"session_id"`
	TaskID       string         `json:"task_id"`
	RequestID    string         `json:"request_id,omitempty"`
	UserID       string         `json:"user_id,omitempty"`
	CollectionID string         `json:"collection_id,omitempty"`
	Items        []kbIngestItem `json:"items"`
}

type kbIngestItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Source  string `json:"source"`
}

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// notifySearchCompletion: API §4.1 搜索完成后回调 Voice Agent；若配置 KB_INGEST_URL 则按接口分配回注 kb。
func (a *App) notifySearchCompletion(
	taskID, sessionID, userID string,
	pipelineStatus string,
	persistFailed bool,
	results []SearchResultItem,
	summary string,
) {
	a.postVoiceSearchCallback(strings.TrimSpace(taskID), strings.TrimSpace(sessionID), pipelineStatus, persistFailed, results, summary)

	if persistFailed || pipelineStatus == "failed" || len(results) == 0 {
		return
	}
	if strings.TrimSpace(a.kbIngestURL) == "" || a.httpCallback == nil {
		return
	}
	a.postKBIngestFromSearch(context.Background(), requestID, taskID, sessionID, userID, results)
}

func (a *App) postVoiceSearchCallback(taskID, sessionID, pipelineStatus string, persistFailed bool, results []SearchResultItem, summary string) {
	url := a.voiceAgentURL + "/api/v1/voice/ppt_message"
	var payload voicePPTMessage
	payload.TaskID = taskID
	payload.SessionID = sessionID
	payload.Priority = "normal"
	payload.EventType = "web_search"

	switch {
	case persistFailed:
		payload.TTSText = "搜索已完成，但结果保存失败，请稍后重试。"
	case pipelineStatus == "failed":
		payload.TTSText = firstNonEmpty(summary, "网络搜索失败")
	default:
		payload.TTSText = formatSearchTTSText(summary, results)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("search voice callback marshal: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("search voice callback request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpCallback.Do(req)
	if err != nil {
		log.Printf("search voice callback POST %s: %v", url, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("search voice callback status=%d task_id=%s", resp.StatusCode, taskID)
	}
}

func formatSearchTTSText(summary string, results []SearchResultItem) string {
	s := strings.TrimSpace(summary)
	if s != "" {
		return s
	}
	if len(results) == 0 {
		return "未检索到相关网页结果。"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("检索到 %d 条结果：", len(results)))
	for i, it := range results {
		if i >= 5 {
			break
		}
		title := strings.TrimSpace(it.Title)
		if title == "" {
			title = "（无标题）"
		}
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(fmt.Sprintf("%d.%s", i+1, title))
	}
	return b.String()
}

func firstNonEmpty(a, b string) string {
	a = strings.TrimSpace(a)
	if a != "" {
		return a
	}
	return strings.TrimSpace(b)
}

func (a *App) postKBIngestFromSearch(ctx context.Context, requestID, taskID, sessionID, userID string, results []SearchResultItem) {
	items := make([]kbIngestItem, 0, len(results))
	for _, r := range results {
		content := strings.TrimSpace(r.Snippet)
		if content == "" {
			content = r.Title
		}
		items = append(items, kbIngestItem{
			Title:   strings.TrimSpace(r.Title),
			URL:     strings.TrimSpace(r.URL),
			Content: content,
			Source:  "web_search",
		})
	}
	if len(items) == 0 {
		return
	}
	payload := kbIngestBody{
		SessionID: sessionID,
		TaskID:    taskID,
		RequestID: requestID,
		UserID:    userID,
		Items:     items,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("kb ingest marshal: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.kbIngestURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("kb ingest request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpCallback.Do(req)
	if err != nil {
		log.Printf("kb ingest POST %s: %v", a.kbIngestURL, err)
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		log.Printf("kb ingest read body: %v", err)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("kb ingest status=%d body=%s", resp.StatusCode, truncateForLog(respBody))
		return
	}
	var env apiEnvelope
	if json.Unmarshal(respBody, &env) == nil && env.Code != 0 && env.Code != 200 {
		log.Printf("kb ingest api code=%d msg=%s", env.Code, env.Message)
	}
}

func truncateForLog(b []byte) string {
	if len(b) <= 500 {
		return string(b)
	}
	return string(b[:500]) + "..."
}
