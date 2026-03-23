package clients

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cfg "voiceagent/internal/config"
	types "voiceagent/internal/types"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *ServiceClients) {
	srv := httptest.NewServer(handler)
	config := &cfg.Config{
		PPTAgentURL:  srv.URL,
		KBServiceURL: srv.URL,
		MemoryURL:    srv.URL,
		SearchURL:    srv.URL,
		DBServiceURL: srv.URL,
	}
	return srv, NewServiceClients(config)
}

func apiOK(data any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d, _ := json.Marshal(data)
		resp := types.APIResponse{Code: 200, Message: "ok", Data: d}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func apiError(code int, msg string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := types.APIResponse{Code: code, Message: msg}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// ===========================================================================
// postJSON / getJSON
// ===========================================================================

func TestPostJSON_Success(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(map[string]string{"result": "ok"}))
	defer srv.Close()

	var out map[string]string
	err := svcClients.postJSON(context.Background(), srv.URL+"/test", map[string]string{"a": "b"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out["result"] != "ok" {
		t.Errorf("result = %v", out)
	}
}

func TestPostJSON_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()
	svcClients := NewServiceClients(&cfg.Config{PPTAgentURL: srv.URL})

	err := svcClients.postJSON(context.Background(), srv.URL+"/test", nil, nil)
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestPostJSON_APIError(t *testing.T) {
	srv, svcClients := newTestServer(apiError(50200, "service unavailable"))
	defer srv.Close()

	var out map[string]string
	err := svcClients.postJSON(context.Background(), srv.URL+"/test", nil, &out)
	if err == nil {
		t.Fatal("expected error for API error code")
	}
}

func TestPostJSON_NilOut(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(nil))
	defer srv.Close()

	err := svcClients.postJSON(context.Background(), srv.URL+"/test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetJSON_Success(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.UserProfile{UserID: "u1", DisplayName: "Test"}))
	defer srv.Close()

	var out types.UserProfile
	err := svcClients.getJSON(context.Background(), srv.URL+"/test", &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.UserID != "u1" {
		t.Errorf("user_id = %q", out.UserID)
	}
}

func TestGetJSON_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()
	svcClients := NewServiceClients(&cfg.Config{PPTAgentURL: srv.URL})

	var out types.UserProfile
	err := svcClients.getJSON(context.Background(), srv.URL+"/test", &out)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// ===========================================================================
// UploadFile
// ===========================================================================

func TestUploadFile_NilBody(t *testing.T) {
	svcClients := NewServiceClients(&cfg.Config{DBServiceURL: "http://localhost:1"})
	r := httptest.NewRequest(http.MethodPost, "/upload", nil)
	r.Body = nil
	_, err := svcClients.UploadFile(r)
	if err == nil {
		t.Fatal("expected error for nil body")
	}
}

func TestUploadFile_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Error("empty body received")
		}
		resp := types.APIResponse{
			Code:    200,
			Message: "ok",
			Data:    json.RawMessage(`{"file_id":"f_uploaded"}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	svcClients := NewServiceClients(&cfg.Config{DBServiceURL: srv.URL})

	r := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("fake file data"))
	r.Header.Set("Content-Type", "multipart/form-data")
	r.Header.Set("Authorization", "Bearer token123")

	data, err := svcClients.UploadFile(r)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	json.Unmarshal(data, &result)
	if result["file_id"] != "f_uploaded" {
		t.Errorf("file_id = %q", result["file_id"])
	}
}

func TestUploadFile_UpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	}))
	defer srv.Close()
	svcClients := NewServiceClients(&cfg.Config{DBServiceURL: srv.URL})

	r := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("data"))
	_, err := svcClients.UploadFile(r)
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestUploadFile_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := types.APIResponse{Code: 40001, Message: "bad request", Data: json.RawMessage(`{"err":"detail"}`)}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	svcClients := NewServiceClients(&cfg.Config{DBServiceURL: srv.URL})

	r := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("data"))
	_, err := svcClients.UploadFile(r)
	if err == nil {
		t.Fatal("expected error for API error")
	}
}

func TestUploadFile_RawResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"file_id":"raw_response"}`))
	}))
	defer srv.Close()
	svcClients := NewServiceClients(&cfg.Config{DBServiceURL: srv.URL})

	r := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("data"))
	data, err := svcClients.UploadFile(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"file_id":"raw_response"}` {
		t.Errorf("data = %s", string(data))
	}
}

// ===========================================================================
// NewServiceClients
// ===========================================================================

func TestNewServiceClients(t *testing.T) {
	config := &cfg.Config{
		PPTAgentURL:  "http://ppt:9100/",
		KBServiceURL: "http://kb:9200/",
		MemoryURL:    "http://mem:9300/",
		SearchURL:    "http://search:9400/",
		DBServiceURL: "http://db:9500/",
	}
	c := NewServiceClients(config)
	if c.pptBaseURL != "http://ppt:9100" {
		t.Errorf("trailing slash not trimmed: %q", c.pptBaseURL)
	}
}
