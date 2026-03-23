package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ===========================================================================
// HandleUpload
// ===========================================================================

func TestHandleUpload_Success(t *testing.T) {
	mock := &mockServices{
		UploadFileFn: func(r *http.Request) (json.RawMessage, error) {
			return json.RawMessage(`{"file_id":"f_uploaded","filename":"test.pdf"}`), nil
		},
	}
	SetGlobalClients(mock)
	defer SetGlobalClients(nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.pdf")
	part.Write([]byte("fake pdf content"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rr := httptest.NewRecorder()
	HandleUpload(rr, req)

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
	HandleUpload(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHandleUpload_WrongContentType(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	HandleUpload(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rr.Code)
	}
}

func TestHandleUpload_NoClients(t *testing.T) {
	SetGlobalClients(nil)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	HandleUpload(rr, req)
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
	SetGlobalClients(mock)
	defer SetGlobalClients(nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "test.pdf")
	part.Write([]byte("content"))
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	HandleUpload(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rr.Code)
	}
}
