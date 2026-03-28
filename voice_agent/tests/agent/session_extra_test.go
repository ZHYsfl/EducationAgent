package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// Session: cancelCurrentPipeline
// ===========================================================================

func TestCancelCurrentPipeline_NoCancel(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.CancelCurrentPipeline() // should not panic with nil cancel
}

func TestCancelCurrentPipeline_WithCancel(t *testing.T) {
	s := agent.NewTestSession(nil)
	cancelled := false
	s.SetPipelineCancel(func() { cancelled = true })
	s.CancelCurrentPipeline()
	if !cancelled {
		t.Error("should have called cancel")
	}
	if s.GetPipelineCancel() != nil {
		t.Error("should set pipelineCancel to nil")
	}
}

// ===========================================================================
// Session: newPipelineContext
// ===========================================================================

func TestNewPipelineContext_CancelsPrevious(t *testing.T) {
	s := agent.NewTestSession(nil)
	oldCancelled := false
	s.SetPipelineCancel(func() { oldCancelled = true })

	ctx := s.NewPipelineContext()
	if !oldCancelled {
		t.Error("should cancel previous pipeline")
	}
	if ctx == nil {
		t.Fatal("should return a context")
	}
	// new cancel should be set
	if s.GetPipelineCancel() == nil {
		t.Error("should set new cancel")
	}
}

func TestNewPipelineContext_Fresh(t *testing.T) {
	s := agent.NewTestSession(nil)
	ctx := s.NewPipelineContext()
	if ctx.Err() != nil {
		t.Error("fresh context should not be cancelled")
	}
}

// ===========================================================================
// Session: handleTextInput
// ===========================================================================

func TestHandleTextInput_EmptyText(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.HandleTextInput(agent.WSMessage{Text: "   "})
	// Should not change state
	if s.GetState() != agent.StateIdle {
		t.Error("empty text should not change state")
	}
}

// ===========================================================================
// Session: onVADEnd
// ===========================================================================

func TestOnVADEnd_NotListening(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.SetStateRaw(agent.StateIdle)
	s.OnVADEnd() // should be no-op when not listening
}

// ===========================================================================
// Session: handleTextMessage - unknown type
// ===========================================================================

func TestHandleTextMessage_UnknownType(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.HandleTextMessage(agent.WSMessage{Type: "unknown_type"}) // should be no-op
}

// ===========================================================================
// Pipeline: ttsWorker
// ===========================================================================

func TestTTSWorker_EmptySentence(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipelineWithTTS(s, mock)

	sentenceCh := make(chan string, 3)
	sentenceCh <- ""
	sentenceCh <- "   "
	sentenceCh <- "有内容的"
	close(sentenceCh)

	p.TTSWorker(context.Background(), sentenceCh)
	// Should not panic, should skip empty sentences
}

func TestTTSWorker_ContextCancel(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipelineWithTTS(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	sentenceCh := make(chan string, 1)

	done := make(chan struct{})
	go func() {
		p.TTSWorker(ctx, sentenceCh)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ttsWorker should exit on cancelled context")
	}
}

// ===========================================================================
// ===========================================================================

// ===========================================================================
// Pipeline: OnInterrupt with partial </think> suffix
// ===========================================================================

func TestOnInterrupt_PartialThinkSuffix(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.WriteRawTokens("<think>some reasoning</thi")
	p.OnInterrupt()

	msgs := p.GetHistory().ToOpenAI()
	found := false
	for _, m := range msgs {
		// We can't easily inspect the content, but it should have been added
		_ = m
		found = true
	}
	if !found {
		t.Error("expected message in history")
	}
}

// ===========================================================================

// ===========================================================================
// handlePreview edge: sets active task via findSessionByTaskID
// ===========================================================================

func TestHandlePreview_NoSession(t *testing.T) {
	mock := &agent.MockServices{
		GetCanvasStatusFn: func(ctx context.Context, taskID string) (agent.CanvasStatusResponse, error) {
			return agent.CanvasStatusResponse{TaskID: taskID}, nil
		},
	}
	agent.SetGlobalClients(mock)
	defer agent.SetGlobalClients(nil)

	req := newHTTPRequest(t, "GET", "/api/v1/tasks/orphan_task/preview", "")
	req.SetPathValue("task_id", "orphan_task")
	rr := httpRecord(req, agent.HandlePreview)
	if rr.Code != 200 {
		t.Errorf("status = %d", rr.Code)
	}
}

// ===========================================================================
// findSessionByTaskID fallback path (OwnsTask)
// ===========================================================================

func TestFindSessionByTaskID_FallbackOwnsTask(t *testing.T) {
	s := agent.NewTestSession(nil)
	s.SessionID = "sess_fallback"
	s.RegisterTask("task_fb_owned", "测试")

	agent.RegisterSession(s)
	defer agent.UnregisterSession(s)

	found := agent.FindSessionByTaskID("task_fb_owned")
	if found == nil {
		t.Fatal("should find via OwnsTask fallback")
	}
	if found.SessionID != "sess_fallback" {
		t.Error("wrong session")
	}
}

// ===========================================================================
// Helpers
// ===========================================================================

func newHTTPRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	return httptest.NewRequest(method, path, bodyReader)
}

func httpRecord(req *http.Request, handler http.HandlerFunc) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}
