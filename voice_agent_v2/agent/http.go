package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Session Registry
// ---------------------------------------------------------------------------

var (
	registryMu      sync.RWMutex
	sessionRegistry = make(map[string]*Session) // session_id → *Session
	taskIndex       = make(map[string]string)   // task_id → session_id

	globalClientsMu sync.RWMutex
	globalClients   ExternalServices
)

func RegisterSession(s *Session) {
	registryMu.Lock()
	sessionRegistry[s.SessionID] = s
	registryMu.Unlock()
}

func UnregisterSession(s *Session) {
	registryMu.Lock()
	if sessionRegistry[s.SessionID] == s {
		delete(sessionRegistry, s.SessionID)
		for tid, sid := range taskIndex {
			if sid == s.SessionID {
				delete(taskIndex, tid)
			}
		}
	}
	registryMu.Unlock()
}

func registerTask(taskID, sessionID string) {
	registryMu.Lock()
	taskIndex[taskID] = sessionID
	registryMu.Unlock()
}

func findSession(taskID string) *Session {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if s, ok := sessionRegistry[taskID]; ok {
		return s
	}
	if sid, ok := taskIndex[taskID]; ok {
		if s, ok := sessionRegistry[sid]; ok {
			return s
		}
	}
	for _, s := range sessionRegistry {
		if s.OwnsTask(taskID) {
			return s
		}
	}
	return nil
}

// SetGlobalClients sets the global ExternalServices (called from main).
func SetGlobalClients(c ExternalServices) {
	globalClientsMu.Lock()
	globalClients = c
	globalClientsMu.Unlock()
}

func getGlobalClients() ExternalServices {
	globalClientsMu.RLock()
	defer globalClientsMu.RUnlock()
	return globalClients
}

// ---------------------------------------------------------------------------
// HTTP Handlers
// ---------------------------------------------------------------------------

// HandlePreview handles GET /api/v1/tasks/{task_id}/preview
func HandlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 40001, "method not allowed")
		return
	}
	taskID := strings.TrimSpace(r.PathValue("task_id"))
	if taskID == "" {
		writeError(w, http.StatusBadRequest, 40001, "task_id is required")
		return
	}
	clients := getGlobalClients()
	if clients == nil {
		writeError(w, http.StatusServiceUnavailable, 50200, "service clients not ready")
		return
	}
	canvas, err := clients.GetCanvasStatus(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusBadGateway, 50200, fmt.Sprintf("fetch canvas status failed: %v", err))
		return
	}

	status := "completed"
	pages := make([]PageInfoBrief, 0, len(canvas.PagesInfo))
	for _, p := range canvas.PagesInfo {
		switch p.Status {
		case "suspended_for_human":
			status = "suspended_for_human"
		case "rendering":
			if status != "suspended_for_human" {
				status = "rendering"
			}
		case "failed":
			status = "failed"
		}
		pages = append(pages, PageInfoBrief{
			PageID:     p.PageID,
			Status:     p.Status,
			LastUpdate: p.LastUpdate,
			RenderURL:  p.RenderURL,
		})
	}
	if s := findSession(taskID); s != nil {
		s.SetActiveTask(taskID)
	}
	writeSuccess(w, http.StatusOK, map[string]any{
		"task_id":                 taskID,
		"status":                  status,
		"page_order":              canvas.PageOrder,
		"current_viewing_page_id": canvas.CurrentViewingPageID,
		"pages":                   pages,
	})
}

// HandlePPTMessage handles POST /api/v1/voice/ppt_message
// Receives async results from PPT Agent, Search, and KB services.
func HandlePPTMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 40001, "method not allowed")
		return
	}
	var req PPTMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 40001, "invalid json body")
		return
	}
	req.TaskID = strings.TrimSpace(req.TaskID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.EventType = strings.TrimSpace(req.EventType)
	if req.TaskID == "" || req.SessionID == "" || req.EventType == "" {
		writeError(w, http.StatusBadRequest, 40001, "task_id, session_id and event_type are required")
		return
	}

	s := findSession(req.TaskID)
	if s == nil || s.SessionID != req.SessionID {
		writeSuccess(w, http.StatusOK, map[string]any{"accepted": true, "delivered": false})
		return
	}

	priority := req.Priority
	if priority == "" {
		priority = "normal"
	}
	if req.EventType == "conflict_question" {
		priority = "high"
	}

	content := strings.TrimSpace(req.TTSText)
	if content == "" {
		content = strings.TrimSpace(req.Summary)
	}
	if content == "" {
		content = "PPT 状态已更新"
	}
	msg := ContextMessage{
		EventType: req.EventType,
		Priority:  priority,
		Content:   content,
		Metadata: map[string]string{
			"session_id": req.SessionID,
			"task_id":    req.TaskID,
			"page_id":    req.PageID,
			"context_id": req.ContextID,
		},
		Timestamp: time.Now().Unix(),
	}
	s.pipeline.EnqueueContext(msg)

	// Forward status events to frontend via WebSocket
	switch req.EventType {
	case "conflict_question":
		s.SendJSON(WSMessage{
			Type: "conflict_ask", TaskID: req.TaskID,
			PageID: req.PageID, ContextID: req.ContextID, Question: content,
		})
	case "ppt_status":
		s.SendJSON(WSMessage{Type: "task_status", TaskID: req.TaskID, Status: req.Status, Progress: req.Progress, Text: content})
	case "page_rendered":
		s.SendJSON(WSMessage{Type: "page_rendered", TaskID: req.TaskID, PageID: req.PageID, RenderURL: req.RenderURL, PageIndex: req.PageIndex})
	case "ppt_preview":
		s.SendJSON(WSMessage{Type: "ppt_preview", TaskID: req.TaskID, PageOrder: req.PageOrder, PagesInfo: req.PagesInfo})
	case "export_ready":
		s.SendJSON(WSMessage{Type: "export_ready", TaskID: req.TaskID, DownloadURL: req.DownloadURL, Format: req.Format})
	case "web_search":
		s.SendJSON(WSMessage{Type: "search_result", TaskID: req.TaskID, Text: content})
	case "kb_query":
		s.SendJSON(WSMessage{Type: "kb_result", TaskID: req.TaskID, Text: content})
	case "get_memory":
		s.SendJSON(WSMessage{Type: "memory_result", TaskID: req.TaskID, Text: content})
	case "error":
		code := req.ErrorCode
		if code == 0 {
			code = 50000
		}
		s.SendJSON(WSMessage{Type: "error", Code: code, Message: content})
	}

	writeSuccess(w, http.StatusOK, map[string]any{"accepted": true, "delivered": true})
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func writeSuccess(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success", "data": data})
}

func writeError(w http.ResponseWriter, httpStatus, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(APIResponse{Code: code, Message: message})
}
