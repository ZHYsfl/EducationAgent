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
	srv, svcClients := newTestServer(apiOK(types.KBQueryResponse{
		Chunks: []types.RetrievedChunk{{ChunkID: "c1", Content: "data", Score: 0.9}},
		Total:  1,
	}))
	defer srv.Close()

	resp, err := svcClients.QueryKB(context.Background(), types.KBQueryRequest{
		UserID: "u1", Query: "test", TopK: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Chunks) != 1 {
		t.Errorf("chunks = %d", len(resp.Chunks))
	}
}

func TestRecallMemory(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.MemoryRecallResponse{
		ProfileSummary: "summary",
	}))
	defer srv.Close()

	resp, err := svcClients.RecallMemory(context.Background(), types.MemoryRecallRequest{
		UserID: "u1", Query: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ProfileSummary != "summary" {
		t.Errorf("summary = %q", resp.ProfileSummary)
	}
}

func TestGetUserProfile(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.UserProfile{UserID: "u1", Subject: "数学"}))
	defer srv.Close()

	profile, err := svcClients.GetUserProfile(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Subject != "数学" {
		t.Errorf("subject = %q", profile.Subject)
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

func TestExtractMemory(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.MemoryExtractResponse{
		ExtractedFacts: []string{"fact1"},
	}))
	defer srv.Close()

	resp, err := svcClients.ExtractMemory(context.Background(), types.MemoryExtractRequest{
		UserID: "u1", SessionID: "s1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ExtractedFacts) != 1 {
		t.Errorf("facts = %d", len(resp.ExtractedFacts))
	}
}

func TestSaveWorkingMemory(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(nil))
	defer srv.Close()

	err := svcClients.SaveWorkingMemory(context.Background(), types.WorkingMemorySaveRequest{
		SessionID: "s1", UserID: "u1",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetWorkingMemory(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(types.WorkingMemory{
		SessionID: "s1", ConversationSummary: "summary",
	}))
	defer srv.Close()

	mem, err := svcClients.GetWorkingMemory(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if mem.ConversationSummary != "summary" {
		t.Errorf("summary = %q", mem.ConversationSummary)
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

func TestIngestFromSearch(t *testing.T) {
	srv, svcClients := newTestServer(apiOK(nil))
	defer srv.Close()

	err := svcClients.IngestFromSearch(context.Background(), types.IngestFromSearchRequest{
		UserID: "u1",
		Items:  []types.SearchIngestItem{{Title: "t1", URL: "http://a.com"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}
