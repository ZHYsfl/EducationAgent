package clients_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"voiceagent/internal/clients"
	cfg "voiceagent/internal/config"
	types "voiceagent/internal/types"
)

func TestQueryKB(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.KBQueryResponse{Accepted: true}))
	defer srv.Close()

	resp, err := svcClients.QueryKB(context.Background(), types.KBQueryRequest{
		UserID: "u1", Query: "test", TopK: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Accepted {
		t.Error("expected accepted=true")
	}
}

func TestRecallMemory(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.MemoryRecallResponse{Accepted: true}))
	defer srv.Close()

	resp, err := svcClients.RecallMemory(context.Background(), types.MemoryRecallRequest{
		UserID: "u1", Query: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Accepted {
		t.Errorf("expected accepted=true")
	}
}

func TestPushContext(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(nil))
	defer srv.Close()

	err := svcClients.PushContext(context.Background(), types.PushContextRequest{
		UserID: "u1", SessionID: "s1",
		Messages: []types.ConversationTurn{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchWeb(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.SearchResponse{
		Summary: "found results",
		Results: []types.SearchResult{{Title: "Result1"}},
	}))
	defer srv.Close()

	resp, err := svcClients.SearchWeb(context.Background(), types.SearchRequest{
		UserID: "u1", Query: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 {
		t.Errorf("results = %d", len(resp.Results))
	}
}

func TestInitPPT(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.PPTInitResponse{TaskID: "task_new"}))
	defer srv.Close()

	resp, err := svcClients.InitPPT(context.Background(), types.PPTInitRequest{
		UserID: "u1", Topic: "数学",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.TaskID != "task_new" {
		t.Errorf("task_id = %q", resp.TaskID)
	}
}

func TestSendFeedback(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(nil))
	defer srv.Close()

	err := svcClients.SendFeedback(context.Background(), types.PPTFeedbackRequest{
		TaskID: "t1", RawText: "修改内容",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetCanvasStatus(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.CanvasStatusResponse{
		TaskID:    "t1",
		PageOrder: []string{"p1"},
	}))
	defer srv.Close()

	resp, err := svcClients.GetCanvasStatus(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.TaskID != "t1" {
		t.Errorf("task_id = %q", resp.TaskID)
	}
}

func TestNotifyVADEvent(t *testing.T) {
	var received types.VADEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		resp := types.APIResponse{Code: 200}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	svcClients := clients.NewServiceClients(&cfg.Config{PPTAgentURL: srv.URL})

	err := svcClients.NotifyVADEvent(context.Background(), types.VADEvent{
		TaskID: "t1", Timestamp: 12345, ViewingPageID: "p1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.TaskID != "t1" {
		t.Errorf("task_id = %q", received.TaskID)
	}
}
