package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ===========================================================================
// HandlePreview
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
	SetGlobalClients(mock)
	defer SetGlobalClients(nil)

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
	HandlePreview(rr, req)

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
	SetGlobalClients(mock)
	defer SetGlobalClients(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_done/preview", nil)
	req.SetPathValue("task_id", "task_done")
	rr := httptest.NewRecorder()
	HandlePreview(rr, req)

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
	SetGlobalClients(mock)
	defer SetGlobalClients(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_fail/preview", nil)
	req.SetPathValue("task_id", "task_fail")
	rr := httptest.NewRecorder()
	HandlePreview(rr, req)

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
	HandlePreview(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandlePreview_EmptyTaskID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks//preview", nil)
	req.SetPathValue("task_id", "")
	rr := httptest.NewRecorder()
	HandlePreview(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}
