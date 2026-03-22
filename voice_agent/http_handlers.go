package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	sessionRegistryMu sync.RWMutex
	sessionRegistry   = make(map[string]*Session) // session_id → *Session
	taskIndex         = make(map[string]string)   // task_id → session_id

	globalClientsMu sync.RWMutex
	globalClients   ExternalServices
)

func registerSession(s *Session) {
	sessionRegistryMu.Lock()
	sessionRegistry[s.SessionID] = s
	sessionRegistryMu.Unlock()
}

func unregisterSession(s *Session) {
	sessionRegistryMu.Lock()
	if sessionRegistry[s.SessionID] == s {
		delete(sessionRegistry, s.SessionID)
		for _, tid := range s.GetAllTasks() {
			if taskIndex[tid] == s.SessionID {
				delete(taskIndex, tid)
			}
		}
	}
	sessionRegistryMu.Unlock()
}

func registerTask(taskID, sessionID string) {
	sessionRegistryMu.Lock()
	taskIndex[taskID] = sessionID
	sessionRegistryMu.Unlock()
}

func findSessionByTaskID(taskID string) *Session {
	sessionRegistryMu.RLock()
	defer sessionRegistryMu.RUnlock()

	if sid, ok := taskIndex[taskID]; ok {
		if s, exists := sessionRegistry[sid]; exists {
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

func setGlobalClients(c ExternalServices) {
	globalClientsMu.Lock()
	globalClients = c
	globalClientsMu.Unlock()
}

func getGlobalClients() ExternalServices {
	globalClientsMu.RLock()
	defer globalClientsMu.RUnlock()
	return globalClients
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 40001, "method not allowed")
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		writeError(w, http.StatusBadRequest, 40001, "content-type must be multipart/form-data")
		return
	}
	clients := getGlobalClients()
	if clients == nil {
		writeError(w, http.StatusServiceUnavailable, 50200, "service clients not ready")
		return
	}
	data, err := clients.UploadFile(r)
	if err != nil {
		writeError(w, http.StatusBadGateway, 50200, fmt.Sprintf("upload gateway failed: %v", err))
		return
	}
	writeRawData(w, http.StatusOK, 200, "success", data)
}

func handlePreview(w http.ResponseWriter, r *http.Request) {
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
	ctx := r.Context()
	canvas, err := clients.GetCanvasStatus(ctx, taskID)
	if err != nil {
		writeError(w, http.StatusBadGateway, 50200, fmt.Sprintf("fetch canvas status failed: %v", err))
		return
	}
	// 顶层 status：任务级汇总，与每页 pages[].status 不同。
	// 规则与《系统接口规范》§2.3 GET /api/v1/tasks/{task_id}/preview 示例一致：
	// 任一页 failed → failed；否则任一页 rendering/suspended_for_human → generating；否则 completed。
	status := "completed"
	pages := make([]PageInfoBrief, 0, len(canvas.PagesInfo))
	for _, p := range canvas.PagesInfo {
		if p.Status == "rendering" || p.Status == "suspended_for_human" {
			status = "generating"
		}
		if p.Status == "failed" {
			status = "failed"
		}
		pages = append(pages, PageInfoBrief{
			PageID:     p.PageID,
			Status:     p.Status,
			LastUpdate: p.LastUpdate,
			RenderURL:  p.RenderURL,
		})
	}
	if s := findSessionByTaskID(taskID); s != nil {
		s.SetActiveTask(taskID)
	}
	payload := map[string]any{
		"task_id":                 taskID,
		"status":                  status,
		"page_order":              canvas.PageOrder,
		"current_viewing_page_id": canvas.CurrentViewingPageID,
		"pages":                   pages,
		"pages_info":              pages,
	}
	writeSuccess(w, http.StatusOK, payload)
}

func handlePPTMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, 40001, "method not allowed")
		return
	}
	var req PPTMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, 40001, "invalid json body")
		return
	}
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, 40001, "task_id is required")
		return
	}
	s := findSessionByTaskID(req.TaskID)
	if s == nil || s.pipeline == nil {
		writeSuccess(w, http.StatusOK, map[string]any{
			"accepted":  true,
			"delivered": false,
		})
		return
	}

	msgType := strings.TrimSpace(req.MsgType)
	if msgType == "" {
		msgType = "tool_result"
	}
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = "normal"
	}
	if msgType == "conflict_question" {
		priority = "high"
	}
	content := strings.TrimSpace(req.TTSText)
	if content == "" {
		content = "PPT 状态已更新"
	}

	msg := ContextMessage{
		ID:       NewID("ctx_"),
		Source:   "ppt_agent",
		Priority: priority,
		MsgType:  msgType,
		Content:  content,
		Metadata: map[string]string{
			"task_id":    req.TaskID,
			"page_id":    req.PageID,
			"context_id": req.ContextID,
		},
		Timestamp: time.Now().UnixMilli(),
	}
	s.pipeline.enqueueContextMessage(r.Context(), msg)

	switch msgType {
	case "ppt_status":
		st := strings.TrimSpace(req.Status)
		if st == "" {
			st = "updated"
		}
		s.SendJSON(WSMessage{
			Type:     "task_status",
			TaskID:   req.TaskID,
			Status:   st,
			Progress: req.Progress,
			Text:     content,
		})
	case "page_rendered":
		s.SendJSON(WSMessage{
			Type:      "page_rendered",
			TaskID:    req.TaskID,
			PageID:    req.PageID,
			RenderURL: req.RenderURL,
			PageIndex: req.PageIndex,
		})
	case "ppt_preview":
		s.SendJSON(WSMessage{
			Type:      "ppt_preview",
			TaskID:    req.TaskID,
			PageOrder: req.PageOrder,
			PagesInfo: req.PagesInfo,
		})
	case "export_ready":
		s.SendJSON(WSMessage{
			Type:        "export_ready",
			TaskID:      req.TaskID,
			DownloadURL: req.DownloadURL,
			Format:      req.Format,
		})
	case "error":
		code := req.ErrorCode
		if code == 0 {
			code = 50000
		}
		s.SendJSON(WSMessage{
			Type:    "error",
			Code:    code,
			Message: content,
		})
	}

	writeSuccess(w, http.StatusOK, map[string]any{
		"accepted": true,
	})
}

func writeSuccess(w http.ResponseWriter, httpStatus int, data any) {
	raw, _ := json.Marshal(data)
	writeRawData(w, httpStatus, 200, "success", raw)
}

func writeError(w http.ResponseWriter, httpStatus, code int, message string) {
	resp := APIResponse{
		Code:    code,
		Message: message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(resp)
}

func writeRawData(w http.ResponseWriter, httpStatus, code int, message string, data json.RawMessage) {
	resp := APIResponse{
		Code:    code,
		Message: message,
		Data:    data,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(resp)
}
