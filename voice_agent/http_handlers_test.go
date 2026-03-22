package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// handleUpload
// ===========================================================================

func TestHandleUpload_Success(t *testing.T) {
	mock := &mockServices{
		UploadFileFn: func(r *http.Request) (json.RawMessage, error) {
			return json.RawMessage(`{"file_id":"f_uploaded","filename":"test.pdf"}`), nil
		},
	}
	setGlobalClients(mock)
	defer setGlobalClients(nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.pdf")
	part.Write([]byte("fake pdf content"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rr := httptest.NewRecorder()
	handleUpload(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rr.Code, rr.Body.String())
	}

	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 200 {
		t.Errorf("code = %d", resp.Code)
	}
}

func TestHandleUpload_WrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/upload", nil)
	rr := httptest.NewRecorder()
	handleUpload(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandleUpload_WrongContentType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handleUpload(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleUpload_NoClients(t *testing.T) {
	setGlobalClients(nil)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	handleUpload(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rr.Code)
	}
}

func TestHandleUpload_GatewayError(t *testing.T) {
	mock := &mockServices{
		UploadFileFn: func(r *http.Request) (json.RawMessage, error) {
			return nil, fmt.Errorf("upstream down")
		},
	}
	setGlobalClients(mock)
	defer setGlobalClients(nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.pdf")
	part.Write([]byte("content"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	handleUpload(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rr.Code)
	}
}

// ===========================================================================
// handlePreview
// ===========================================================================

func TestHandlePreview_Success(t *testing.T) {
	mock := &mockServices{
		GetCanvasStatusFn: func(ctx context.Context, taskID string) (CanvasStatusResponse, error) {
			return CanvasStatusResponse{
				TaskID:    taskID,
				PageOrder: []string{"p1", "p2"},
				PagesInfo: []PageStatusInfo{
					{PageID: "p1", Status: "completed", RenderURL: "http://r/p1"},
					{PageID: "p2", Status: "rendering", RenderURL: ""},
				},
			}, nil
		},
	}
	setGlobalClients(mock)
	defer setGlobalClients(nil)

	// Create a session that owns this task so SetActiveTask works
	s := newTestSession(mock)
	s.SessionID = "sess_preview"
	s.RegisterTask("task_preview", "预览测试")
	registerSession(s)
	registerTask("task_preview", s.SessionID)
	defer unregisterSession(s)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_preview/preview", nil)
	req.SetPathValue("task_id", "task_preview")
	rr := httptest.NewRecorder()
	handlePreview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rr.Code, rr.Body.String())
	}

	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 200 {
		t.Errorf("code = %d", resp.Code)
	}

	var data map[string]any
	json.Unmarshal(resp.Data, &data)
	if data["status"] != "generating" {
		t.Errorf("status = %v, want generating (has rendering page)", data["status"])
	}
}

func TestHandlePreview_AllCompleted(t *testing.T) {
	mock := &mockServices{
		GetCanvasStatusFn: func(ctx context.Context, taskID string) (CanvasStatusResponse, error) {
			return CanvasStatusResponse{
				TaskID:    taskID,
				PageOrder: []string{"p1"},
				PagesInfo: []PageStatusInfo{
					{PageID: "p1", Status: "completed", RenderURL: "http://r/p1"},
				},
			}, nil
		},
	}
	setGlobalClients(mock)
	defer setGlobalClients(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_done/preview", nil)
	req.SetPathValue("task_id", "task_done")
	rr := httptest.NewRecorder()
	handlePreview(rr, req)

	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	var data map[string]any
	json.Unmarshal(resp.Data, &data)
	if data["status"] != "completed" {
		t.Errorf("status = %v, want completed", data["status"])
	}
}

func TestHandlePreview_FailedPage(t *testing.T) {
	mock := &mockServices{
		GetCanvasStatusFn: func(ctx context.Context, taskID string) (CanvasStatusResponse, error) {
			return CanvasStatusResponse{
				TaskID: taskID,
				PagesInfo: []PageStatusInfo{
					{PageID: "p1", Status: "failed"},
				},
			}, nil
		},
	}
	setGlobalClients(mock)
	defer setGlobalClients(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_fail/preview", nil)
	req.SetPathValue("task_id", "task_fail")
	rr := httptest.NewRecorder()
	handlePreview(rr, req)

	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	var data map[string]any
	json.Unmarshal(resp.Data, &data)
	if data["status"] != "failed" {
		t.Errorf("status = %v, want failed", data["status"])
	}
}

func TestHandlePreview_WrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/x/preview", nil)
	rr := httptest.NewRecorder()
	handlePreview(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandlePreview_EmptyTaskID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks//preview", nil)
	req.SetPathValue("task_id", "")
	rr := httptest.NewRecorder()
	handlePreview(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

// ===========================================================================
// handlePPTMessage
// ===========================================================================

func TestHandlePPTMessage_PPTStatus(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_ppt_msg"
	s.RegisterTask("task_msg_1", "测试PPT")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_msg_1", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_msg_1","msg_type":"ppt_status","tts_text":"生成中","status":"rendering","progress":50}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rr.Code, rr.Body.String())
	}

	time.Sleep(30 * time.Millisecond)
	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "task_status")
	if !ok {
		t.Fatal("expected task_status WS message")
	}
	if found.Progress != 50 {
		t.Errorf("progress = %d", found.Progress)
	}
	if found.Status != "rendering" {
		t.Errorf("status = %q", found.Status)
	}
}

func TestHandlePPTMessage_PageRendered(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_page_r"
	s.RegisterTask("task_pg", "测试")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_pg", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_pg","msg_type":"page_rendered","page_id":"p1","render_url":"http://r/p1","page_index":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	time.Sleep(30 * time.Millisecond)
	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "page_rendered")
	if !ok {
		t.Fatal("expected page_rendered")
	}
	if found.RenderURL != "http://r/p1" {
		t.Errorf("render_url = %q", found.RenderURL)
	}
}

func TestHandlePPTMessage_PPTPreview(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_preview_msg"
	s.RegisterTask("task_pv", "测试")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_pv", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_pv","msg_type":"ppt_preview","page_order":["p1","p2"],"pages_info":[{"page_id":"p1","status":"completed","last_update":0,"render_url":"http://r/p1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	time.Sleep(30 * time.Millisecond)
	msgs := drainWriteCh(s)
	_, ok := findWSMessage(msgs, "ppt_preview")
	if !ok {
		t.Fatal("expected ppt_preview")
	}
}

func TestHandlePPTMessage_ExportReady(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_export"
	s.RegisterTask("task_ex", "测试")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_ex", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_ex","msg_type":"export_ready","download_url":"http://dl/ppt.pptx","format":"pptx"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	time.Sleep(30 * time.Millisecond)
	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "export_ready")
	if !ok {
		t.Fatal("expected export_ready")
	}
	if found.DownloadURL != "http://dl/ppt.pptx" {
		t.Errorf("download_url = %q", found.DownloadURL)
	}
}

func TestHandlePPTMessage_Error(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_err"
	s.RegisterTask("task_err", "测试")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_err", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_err","msg_type":"error","tts_text":"渲染失败","error_code":50300}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	time.Sleep(30 * time.Millisecond)
	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "error")
	if !ok {
		t.Fatal("expected error WS message")
	}
	if found.Code != 50300 {
		t.Errorf("code = %d, want 50300", found.Code)
	}
}

func TestHandlePPTMessage_ErrorDefaultCode(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_err2"
	s.RegisterTask("task_err2", "测试")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_err2", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_err2","msg_type":"error","tts_text":"出错了"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	time.Sleep(30 * time.Millisecond)
	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "error")
	if !ok {
		t.Fatal("expected error WS message")
	}
	if found.Code != 50000 {
		t.Errorf("code = %d, want default 50000", found.Code)
	}
}

func TestHandlePPTMessage_ConflictQuestion_ForceHighPriority(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_conflict"
	s.RegisterTask("task_cf", "测试")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_cf", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_cf","msg_type":"conflict_question","tts_text":"用什么颜色？","context_id":"ctx_cf1","priority":"normal"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}

	// The message should be in the high priority queue
	select {
	case msg := <-p.highPriorityQueue:
		if msg.Priority != "high" {
			t.Errorf("priority = %q, want high (forced)", msg.Priority)
		}
		if msg.MsgType != "conflict_question" {
			t.Errorf("msg_type = %q", msg.MsgType)
		}
	case <-time.After(time.Second):
		t.Fatal("expected message in high priority queue")
	}
}

func TestHandlePPTMessage_DefaultMsgType(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_default"
	s.RegisterTask("task_df", "测试")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_df", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_df","tts_text":"通知"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	select {
	case msg := <-p.contextQueue:
		if msg.MsgType != "tool_result" {
			t.Errorf("default msg_type = %q, want tool_result", msg.MsgType)
		}
	case <-time.After(time.Second):
		t.Fatal("expected message in context queue")
	}
}

func TestHandlePPTMessage_NoSession(t *testing.T) {
	body := `{"task_id":"task_orphan","msg_type":"ppt_status","tts_text":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	var data map[string]any
	json.Unmarshal(resp.Data, &data)
	if data["delivered"] != false {
		t.Error("delivered should be false when no session found")
	}
}

func TestHandlePPTMessage_EmptyTaskID(t *testing.T) {
	body := `{"task_id":"","msg_type":"ppt_status"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandlePPTMessage_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandlePPTMessage_WrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/voice/ppt_message", nil)
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandlePPTMessage_DefaultTTSText(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SessionID = "sess_notts"
	s.RegisterTask("task_notts", "测试")
	p := newTestPipeline(s, mock)
	_ = p

	registerSession(s)
	registerTask("task_notts", s.SessionID)
	defer unregisterSession(s)

	body := `{"task_id":"task_notts","msg_type":"ppt_status","status":"done"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handlePPTMessage(rr, req)

	select {
	case msg := <-p.contextQueue:
		if msg.Content != "PPT 状态已更新" {
			t.Errorf("default content = %q", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("expected message")
	}
}

// ===========================================================================
// writeSuccess / writeError / writeRawData
// ===========================================================================

func TestWriteSuccess(t *testing.T) {
	rr := httptest.NewRecorder()
	writeSuccess(rr, http.StatusOK, map[string]string{"key": "value"})
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d", rr.Code)
	}
	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 200 {
		t.Errorf("code = %d", resp.Code)
	}
}

func TestWriteError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeError(rr, http.StatusBadRequest, 40001, "bad request")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rr.Code)
	}
	var resp APIResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Code != 40001 {
		t.Errorf("code = %d", resp.Code)
	}
	if resp.Message != "bad request" {
		t.Errorf("message = %q", resp.Message)
	}
}

// ===========================================================================
// CORS middleware
// ===========================================================================

func TestWithCORS(t *testing.T) {
	handler := withCORS(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing CORS header")
	}
}

func TestWithCORS_Preflight(t *testing.T) {
	handler := withCORS(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rr.Code)
	}
}

func TestPreflightOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	rr := httptest.NewRecorder()
	preflightOnly(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rr.Code)
	}
}

// ===========================================================================
// globalClients
// ===========================================================================

func TestSetGetGlobalClients(t *testing.T) {
	mock := &mockServices{}
	setGlobalClients(mock)
	got := getGlobalClients()
	if got != mock {
		t.Error("get should return what was set")
	}
	setGlobalClients(nil)
	if getGlobalClients() != nil {
		t.Error("should be nil after clearing")
	}
}
