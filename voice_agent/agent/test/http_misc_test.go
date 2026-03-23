package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
// globalClients
// ===========================================================================

func TestSetGetGlobalClients(t *testing.T) {
	mock := &mockServices{}
	SetGlobalClients(mock)
	got := getGlobalClients()
	if got != mock {
		t.Error("get should return what was set")
	}
	SetGlobalClients(nil)
	if getGlobalClients() != nil {
		t.Error("should be nil after clearing")
	}
}
