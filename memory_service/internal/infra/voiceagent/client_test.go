package voiceagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"memory_service/internal/service"
)

func TestSendPPTMessageUsesCanonicalRecallCallbackFields(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "/api/v1/voice/ppt_message", "", time.Second, 1)
	err := client.SendPPTMessage(context.Background(), service.VoicePPTMessageRequest{
		TaskID:    "sess_1",
		SessionID: "sess_1",
		RequestID: "req_1",
		EventType: "get_memory",
		Summary:   "Profile: concise preference",
	})
	if err != nil {
		t.Fatalf("send callback: %v", err)
	}

	if got["task_id"] != "sess_1" || got["session_id"] != "sess_1" {
		t.Fatalf("unexpected task/session ids: %#v", got)
	}
	if got["event_type"] != "get_memory" {
		t.Fatalf("expected event_type=get_memory, got %#v", got["event_type"])
	}
	if got["summary"] != "Profile: concise preference" {
		t.Fatalf("unexpected summary: %#v", got["summary"])
	}
	if _, ok := got["msg_type"]; ok {
		t.Fatalf("msg_type should not be present in canonical callback payload: %#v", got)
	}
}

