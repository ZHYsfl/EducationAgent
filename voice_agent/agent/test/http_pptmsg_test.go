package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// HandlePPTMessage
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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)

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
	HandlePPTMessage(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandlePPTMessage_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/ppt_message", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	HandlePPTMessage(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandlePPTMessage_WrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/voice/ppt_message", nil)
	rr := httptest.NewRecorder()
	HandlePPTMessage(rr, req)
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
	HandlePPTMessage(rr, req)

	select {
	case msg := <-p.contextQueue:
		if msg.Content != "PPT 状态已更新" {
			t.Errorf("default content = %q", msg.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("expected message")
	}
}
