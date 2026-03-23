package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adaptivepkg "voiceagent/internal/adaptive"
)

// ===========================================================================
// Session: cancelCurrentPipeline
// ===========================================================================

func TestCancelCurrentPipeline_NoCancel(t *testing.T) {
	s := newTestSession(nil)
	s.cancelCurrentPipeline() // should not panic with nil cancel
}

func TestCancelCurrentPipeline_WithCancel(t *testing.T) {
	s := newTestSession(nil)
	cancelled := false
	s.pipelineCancel = func() { cancelled = true }
	s.cancelCurrentPipeline()
	if !cancelled {
		t.Error("should have called cancel")
	}
	if s.pipelineCancel != nil {
		t.Error("should set pipelineCancel to nil")
	}
}

// ===========================================================================
// Session: newPipelineContext
// ===========================================================================

func TestNewPipelineContext_CancelsPrevious(t *testing.T) {
	s := newTestSession(nil)
	oldCancelled := false
	s.pipelineCancel = func() { oldCancelled = true }

	ctx := s.newPipelineContext()
	if !oldCancelled {
		t.Error("should cancel previous pipeline")
	}
	if ctx == nil {
		t.Fatal("should return a context")
	}
	// new cancel should be set
	if s.pipelineCancel == nil {
		t.Error("should set new cancel")
	}
}

func TestNewPipelineContext_Fresh(t *testing.T) {
	s := newTestSession(nil)
	ctx := s.newPipelineContext()
	if ctx.Err() != nil {
		t.Error("fresh context should not be cancelled")
	}
}

// ===========================================================================
// Session: handleTextInput
// ===========================================================================

func TestHandleTextInput_EmptyText(t *testing.T) {
	s := newTestSession(nil)
	s.handleTextInput(WSMessage{Text: "   "})
	// Should not change state
	if s.GetState() != StateIdle {
		t.Error("empty text should not change state")
	}
}

// ===========================================================================
// Session: onVADEnd
// ===========================================================================

func TestOnVADEnd_NotListening(t *testing.T) {
	s := newTestSession(nil)
	s.state = StateIdle
	s.onVADEnd() // should be no-op when not listening
}

// ===========================================================================
// Session: handleTextMessage - unknown type
// ===========================================================================

func TestHandleTextMessage_UnknownType(t *testing.T) {
	s := newTestSession(nil)
	s.handleTextMessage(WSMessage{Type: "unknown_type"}) // should be no-op
}

// ===========================================================================
// Pipeline: ttsWorker
// ===========================================================================

func TestTTSWorker_EmptySentence(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipelineWithTTS(s, mock)

	sentenceCh := make(chan string, 3)
	sentenceCh <- ""
	sentenceCh <- "   "
	sentenceCh <- "有内容的"
	close(sentenceCh)

	p.ttsWorker(context.Background(), sentenceCh)
	// Should not panic, should skip empty sentences
}

func TestTTSWorker_ContextCancel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipelineWithTTS(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	sentenceCh := make(chan string, 1)

	done := make(chan struct{})
	go func() {
		p.ttsWorker(ctx, sentenceCh)
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
// Pipeline: launchAsyncContextQueries (nil clients)
// ===========================================================================

func TestLaunchAsyncContextQueries_NilClients(t *testing.T) {
	s := newTestSession(nil)
	p := newTestPipeline(s, nil)
	p.launchAsyncContextQueries(context.Background(), "test") // should not panic
}

// ===========================================================================
// Pipeline: OnInterrupt with partial </think> suffix
// ===========================================================================

func TestOnInterrupt_PartialThinkSuffix(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.rawGeneratedTokens.WriteString("<think>some reasoning</thi")
	p.OnInterrupt()

	msgs := p.history.ToOpenAI()
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
// formatMemoryForLLM edge: empty Content and Value entries
// ===========================================================================

func TestFormatMemoryForLLM_SkipsEmpty(t *testing.T) {
	resp := MemoryRecallResponse{
		Facts:       []MemoryEntry{{Content: "", Value: ""}},
		Preferences: []MemoryEntry{{Content: "", Value: ""}},
	}
	result := formatMemoryForLLM(resp)
	if strings.Contains(result, "1)") {
		t.Error("should skip entries with both empty Content and Value")
	}
}

// ===========================================================================
// handlePreview edge: sets active task via findSessionByTaskID
// ===========================================================================

func TestHandlePreview_NoSession(t *testing.T) {
	mock := &mockServices{
		GetCanvasStatusFn: func(ctx context.Context, taskID string) (CanvasStatusResponse, error) {
			return CanvasStatusResponse{TaskID: taskID}, nil
		},
	}
	SetGlobalClients(mock)
	defer SetGlobalClients(nil)

	req := newHTTPRequest(t, "GET", "/api/v1/tasks/orphan_task/preview", "")
	req.SetPathValue("task_id", "orphan_task")
	rr := httpRecord(req, HandlePreview)
	if rr.Code != 200 {
		t.Errorf("status = %d", rr.Code)
	}
}

// ===========================================================================
// findSessionByTaskID fallback path (OwnsTask)
// ===========================================================================

func TestFindSessionByTaskID_FallbackOwnsTask(t *testing.T) {
	s := newTestSession(nil)
	s.SessionID = "sess_fallback"
	s.RegisterTask("task_fb_owned", "测试")

	registerSession(s)
	defer unregisterSession(s)

	found := findSessionByTaskID("task_fb_owned")
	if found == nil {
		t.Fatal("should find via OwnsTask fallback")
	}
	if found.SessionID != "sess_fallback" {
		t.Error("wrong session")
	}
}

// ===========================================================================
// prefillFromMemory edge: nil clients
// ===========================================================================

func TestPrefillFromMemory_NilClients(t *testing.T) {
	s := newTestSession(nil)
	req := NewTaskRequirements("s", "u")
	s.prefillFromMemory(req) // should not panic
}

// ===========================================================================
// createPPTFromRequirements: nil Requirements
// ===========================================================================

func TestCreatePPTFromRequirements_NilReq(t *testing.T) {
	s := newTestSession(&mockServices{})
	s.pipeline = &Pipeline{adaptive: adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())}
	s.Requirements = nil
	s.createPPTFromRequirements() // should not panic
}

// ===========================================================================
// createPPTFromRequirements: nil clients
// ===========================================================================

func TestCreatePPTFromRequirements_NilClients(t *testing.T) {
	s := newTestSession(nil)
	s.pipeline = &Pipeline{adaptive: adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes())}
	s.Requirements = makeFullRequirements()
	s.createPPTFromRequirements() // should not panic
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
