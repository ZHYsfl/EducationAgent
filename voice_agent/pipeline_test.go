package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ===========================================================================
// drainContextQueue
// ===========================================================================

func TestDrainContextQueue_EmptyQueue(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	msgs := p.drainContextQueue()
	if len(msgs) != 0 {
		t.Errorf("expected 0, got %d", len(msgs))
	}
}

func TestDrainContextQueue_FromChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.contextQueue <- ContextMessage{ID: "c1", Content: "msg1"}
	p.contextQueue <- ContextMessage{ID: "c2", Content: "msg2"}

	msgs := p.drainContextQueue()
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].ID != "c1" || msgs[1].ID != "c2" {
		t.Error("wrong order or content")
	}
}

func TestDrainContextQueue_FromPending(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.pendingContexts = []ContextMessage{
		{ID: "p1", Content: "pending1"},
	}
	p.contextQueue <- ContextMessage{ID: "c1", Content: "channel1"}

	msgs := p.drainContextQueue()
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].ID != "p1" {
		t.Error("pending messages should come first")
	}
	if len(p.pendingContexts) != 0 {
		t.Error("pending should be cleared")
	}
}

// ===========================================================================
// enqueueContextMessage
// ===========================================================================

func TestEnqueueContextMessage_Normal(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.enqueueContextMessage(context.Background(), ContextMessage{
		ID:       "n1",
		Priority: "normal",
	})

	select {
	case msg := <-p.contextQueue:
		if msg.ID != "n1" {
			t.Error("wrong message in normal queue")
		}
	default:
		t.Error("expected message in contextQueue")
	}
}

func TestEnqueueContextMessage_High(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.enqueueContextMessage(context.Background(), ContextMessage{
		ID:       "h1",
		Priority: "high",
	})

	select {
	case msg := <-p.highPriorityQueue:
		if msg.ID != "h1" {
			t.Error("wrong message in high priority queue")
		}
	default:
		t.Error("expected message in highPriorityQueue")
	}
}

func TestEnqueueContextMessage_CancelledContext(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Fill both queues
	for i := 0; i < cap(p.contextQueue); i++ {
		p.contextQueue <- ContextMessage{ID: "filler"}
	}
	for i := 0; i < cap(p.highPriorityQueue); i++ {
		p.highPriorityQueue <- ContextMessage{ID: "filler"}
	}

	// Should not block with cancelled context
	done := make(chan struct{})
	go func() {
		p.enqueueContextMessage(ctx, ContextMessage{ID: "overflow", Priority: "normal"})
		p.enqueueContextMessage(ctx, ContextMessage{ID: "overflow", Priority: "high"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enqueueContextMessage blocked on full queue with cancelled context")
	}
}

// ===========================================================================
// extractContextIDFromResponse
// ===========================================================================

func TestExtractContextIDFromResponse(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with marker", "用户确认了方案A [RESOLVE_CONFLICT:ctx_abc123]", "ctx_abc123"},
		{"no marker", "普通回复没有标记", ""},
		{"marker at end", "[RESOLVE_CONFLICT:ctx_xyz]", "ctx_xyz"},
		{"with spaces", "[RESOLVE_CONFLICT: ctx_spaced ]", "ctx_spaced"},
		{"unclosed", "[RESOLVE_CONFLICT:ctx_unclosed", ""},
		{"empty id", "[RESOLVE_CONFLICT:]", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.extractContextIDFromResponse(tt.input)
			if got != tt.want {
				t.Errorf("extractContextIDFromResponse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// tryDetectTaskInit
// ===========================================================================

func TestTryDetectTaskInit_WithMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	llmResp := `好的，我来帮您制作课件。[TASK_INIT]{"topic":"高等数学"}[/TASK_INIT]`
	detected := p.tryDetectTaskInit(llmResp)
	if !detected {
		t.Fatal("should detect TASK_INIT")
	}

	s.reqMu.RLock()
	req := s.Requirements
	s.reqMu.RUnlock()

	if req == nil {
		t.Fatal("Requirements should be created")
	}
	if req.Topic != "高等数学" {
		t.Errorf("topic = %q, want 高等数学", req.Topic)
	}
	if req.Status != "collecting" {
		t.Errorf("status = %q, want collecting", req.Status)
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "requirements_progress")
	if !ok {
		t.Error("expected requirements_progress WS message")
	}
	if found.Status != "collecting" {
		t.Errorf("ws status = %q", found.Status)
	}
}

func TestTryDetectTaskInit_NoMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	detected := p.tryDetectTaskInit("普通对话没有任何标记")
	if detected {
		t.Error("should not detect TASK_INIT")
	}
}

func TestTryDetectTaskInit_InvalidJSON(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	detected := p.tryDetectTaskInit(`[TASK_INIT]not valid json[/TASK_INIT]`)
	if detected {
		t.Error("invalid JSON should return false")
	}
	if s.Requirements != nil {
		t.Error("Requirements should not be set for invalid JSON")
	}
}

func TestTryDetectTaskInit_EmptyTopic(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	detected := p.tryDetectTaskInit(`[TASK_INIT]{"topic":""}[/TASK_INIT]`)
	if !detected {
		t.Error("should still detect even with empty topic")
	}
	s.reqMu.RLock()
	req := s.Requirements
	s.reqMu.RUnlock()
	if req.Topic != "" {
		t.Errorf("topic should be empty, got %q", req.Topic)
	}
}

// ===========================================================================
// trySendPPTFeedback
// ===========================================================================

func TestTrySendPPTFeedback_WithMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.RegisterTask("task_fb", "数学")
	s.SetActiveTask("task_fb")
	s.activeTaskMu.Lock()
	s.ViewingPageID = "page_fb"
	s.LastVADTimestamp = 12345
	s.activeTaskMu.Unlock()

	llmResp := `好的，我帮您修改。[PPT_FEEDBACK]{"action_type":"modify","page_id":"page_fb","instruction":"字体改大","scope":"page","keywords":["字体"]}[/PPT_FEEDBACK]`
	p.trySendPPTFeedback("把字体改大一点", llmResp)

	calls := waitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1 feedback call, got %d", len(calls))
	}
	fb := calls[0]
	if fb.TaskID != "task_fb" {
		t.Errorf("task_id = %q", fb.TaskID)
	}
	if fb.BaseTimestamp != 12345 {
		t.Errorf("base_timestamp = %d, want 12345", fb.BaseTimestamp)
	}
	if fb.ViewingPageID != "page_fb" {
		t.Errorf("viewing_page_id = %q", fb.ViewingPageID)
	}
	if len(fb.Intents) != 1 {
		t.Fatalf("expected 1 intent, got %d", len(fb.Intents))
	}
	intent := fb.Intents[0]
	if intent.ActionType != "modify" {
		t.Errorf("action_type = %q", intent.ActionType)
	}
	if intent.Instruction != "字体改大" {
		t.Errorf("instruction = %q", intent.Instruction)
	}
}

func TestTrySendPPTFeedback_NoMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	s.RegisterTask("t1", "数学")
	s.SetActiveTask("t1")

	p.trySendPPTFeedback("你好", "你好，有什么可以帮您的吗？")
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := len(mock.feedbackCalls)
	mock.mu.Unlock()
	if calls != 0 {
		t.Error("should not send feedback without marker")
	}
}

func TestTrySendPPTFeedback_NoActiveTask(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.trySendPPTFeedback("改一下", `[PPT_FEEDBACK]{"action_type":"modify"}[/PPT_FEEDBACK]`)
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := len(mock.feedbackCalls)
	mock.mu.Unlock()
	if calls != 0 {
		t.Error("should not send feedback without active task")
	}
}

func TestTrySendPPTFeedback_InvalidJSON(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	s.RegisterTask("t1", "数学")
	s.SetActiveTask("t1")

	p.trySendPPTFeedback("改一下", `[PPT_FEEDBACK]invalid json[/PPT_FEEDBACK]`)
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := len(mock.feedbackCalls)
	mock.mu.Unlock()
	if calls != 0 {
		t.Error("invalid JSON should not trigger feedback")
	}
}

func TestTrySendPPTFeedback_FallbackPageID(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	s.RegisterTask("t1", "数学")
	s.SetActiveTask("t1")
	s.activeTaskMu.Lock()
	s.ViewingPageID = "current_page"
	s.activeTaskMu.Unlock()

	p.trySendPPTFeedback("改背景", `[PPT_FEEDBACK]{"action_type":"style","instruction":"改背景色"}[/PPT_FEEDBACK]`)
	calls := waitForFeedback(mock, 1)
	if len(calls) == 0 {
		t.Fatal("expected feedback call")
	}
	if calls[0].Intents[0].PageID != "current_page" {
		t.Errorf("should fallback to viewing page, got %q", calls[0].Intents[0].PageID)
	}
}

// ===========================================================================
// tryResolveConflict
// ===========================================================================

func TestTryResolveConflict_SinglePending(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_conflict_1", "task_c1")
	s.activeTaskMu.Lock()
	s.ViewingPageID = "pg1"
	s.LastVADTimestamp = 99999
	s.activeTaskMu.Unlock()

	resolved := p.tryResolveConflict(context.Background(), "选方案A", "用户选了方案A")
	if !resolved {
		t.Error("should resolve with single pending question")
	}

	calls := waitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1 feedback, got %d", len(calls))
	}
	fb := calls[0]
	if fb.TaskID != "task_c1" {
		t.Errorf("task_id = %q", fb.TaskID)
	}
	if fb.ReplyToContextID != "ctx_conflict_1" {
		t.Errorf("reply_to_context_id = %q", fb.ReplyToContextID)
	}
	if fb.BaseTimestamp != 99999 {
		t.Errorf("base_timestamp = %d", fb.BaseTimestamp)
	}
	if fb.Intents[0].ActionType != "resolve_conflict" {
		t.Errorf("action_type = %q", fb.Intents[0].ActionType)
	}

	// Pending question should be removed
	_, ok := s.ResolvePendingQuestion("ctx_conflict_1")
	if ok {
		t.Error("pending question should have been consumed")
	}
}

func TestTryResolveConflict_MultiplePending_WithMarker(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_a", "task_1")
	s.AddPendingQuestion("ctx_b", "task_2")

	llmResp := "好的选方案B [RESOLVE_CONFLICT:ctx_b]"
	resolved := p.tryResolveConflict(context.Background(), "选方案B", llmResp)
	if !resolved {
		t.Error("should resolve with marker")
	}

	calls := waitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1, got %d", len(calls))
	}
	if calls[0].ReplyToContextID != "ctx_b" {
		t.Errorf("should resolve ctx_b, got %q", calls[0].ReplyToContextID)
	}
}

func TestTryResolveConflict_NoPending(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	resolved := p.tryResolveConflict(context.Background(), "hi", "hello")
	if resolved {
		t.Error("should not resolve with no pending questions")
	}
}

func TestTryResolveConflict_NilClients(t *testing.T) {
	s := newTestSession(nil)
	p := newTestPipeline(s, nil)

	s.AddPendingQuestion("ctx_1", "task_1")
	resolved := p.tryResolveConflict(context.Background(), "hi", "hello")
	if resolved {
		t.Error("nil clients should return false")
	}
}

// ===========================================================================
// handleRequirementsTransition
// ===========================================================================

func TestHandleRequirementsTransition_CollectingToConfirming(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	req := makeFullRequirements()
	req.Status = "collecting"
	s.Requirements = req

	p.handleRequirementsTransition("好的信息齐全了 [REQUIREMENTS_CONFIRMED]")

	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()
	if status != "confirming" {
		t.Errorf("status = %q, want confirming", status)
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "requirements_summary")
	if !ok {
		t.Fatal("expected requirements_summary WS message")
	}
	if found.SummaryText == "" {
		t.Error("summary text should be non-empty")
	}
}

func TestHandleRequirementsTransition_CollectingStaysCollecting(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	req := NewTaskRequirements("s", "u")
	req.Topic = "测试"
	req.Status = "collecting"
	s.Requirements = req

	p.handleRequirementsTransition("知道了，请告诉我更多信息")

	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()
	if status != "collecting" {
		t.Errorf("status = %q, want collecting", status)
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "requirements_progress")
	if !ok {
		t.Fatal("expected requirements_progress WS message")
	}
	if found.Status != "collecting" {
		t.Errorf("ws status = %q", found.Status)
	}
}

func TestHandleRequirementsTransition_ConfirmingToConfirmed(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipelineWithTTS(s, mock)

	req := makeFullRequirements()
	req.SessionID = s.SessionID
	req.UserID = s.UserID
	req.Status = "confirming"
	s.Requirements = req

	registerSession(s)
	defer unregisterSession(s)

	p.handleRequirementsTransition("用户确认了 [REQUIREMENTS_CONFIRMED]")

	time.Sleep(300 * time.Millisecond)
	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()

	_ = p
	if status != "confirmed" && status != "generating" {
		t.Errorf("status = %q, want confirmed or generating", status)
	}
}

func TestHandleRequirementsTransition_ConfirmingBackToCollecting(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	req := makeFullRequirements()
	req.Status = "confirming"
	s.Requirements = req

	p.handleRequirementsTransition("我要修改一下，先改个主题")

	s.reqMu.RLock()
	status := s.Requirements.Status
	collected := s.Requirements.CollectedFields
	s.reqMu.RUnlock()
	if status != "collecting" {
		t.Errorf("status = %q, want collecting", status)
	}
	if len(collected) == 0 {
		t.Error("RefreshCollectedFields should have been called")
	}
}

func TestHandleRequirementsTransition_NilRequirements(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.handleRequirementsTransition("anything") // should not panic
}

// ===========================================================================
// buildTaskListContext
// ===========================================================================

func TestBuildTaskListContext_NoTasks(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	result := p.buildTaskListContext()
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestBuildTaskListContext_SingleTask(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.RegisterTask("task_t1", "高等数学")
	s.SetActiveTask("task_t1")

	result := p.buildTaskListContext()
	if !strings.Contains(result, "高等数学") {
		t.Error("should contain task topic")
	}
	if !strings.Contains(result, "task_t1") {
		t.Error("should contain task ID")
	}
	if !strings.Contains(result, "当前活跃") {
		t.Error("should mark active task")
	}
}

func TestBuildTaskListContext_MultipleTasks(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.RegisterTask("task_a", "数学")
	s.RegisterTask("task_b", "物理")
	s.SetActiveTask("task_a")

	result := p.buildTaskListContext()
	if !strings.Contains(result, "数学") || !strings.Contains(result, "物理") {
		t.Error("should contain both task topics")
	}
	if !strings.Contains(result, "用户可能用简称") {
		t.Error("should include multi-task instructions for LLM")
	}
}

// ===========================================================================
// buildPendingQuestionsContext
// ===========================================================================

func TestBuildPendingQuestionsContext_NoQuestions(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	result := p.buildPendingQuestionsContext()
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestBuildPendingQuestionsContext_SingleQuestion(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_q1", "task_q1")
	result := p.buildPendingQuestionsContext()
	if !strings.Contains(result, "ctx_q1") {
		t.Error("should contain context_id")
	}
}

func TestBuildPendingQuestionsContext_MultipleQuestions(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_1", "task_1")
	s.AddPendingQuestion("ctx_2", "task_2")
	result := p.buildPendingQuestionsContext()
	if !strings.Contains(result, "RESOLVE_CONFLICT") {
		t.Error("multiple questions should include RESOLVE_CONFLICT instruction")
	}
}

// ===========================================================================
// postProcessResponse dispatching
// ===========================================================================

func TestPostProcessResponse_ConflictFirst(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_pp", "task_pp")
	p.postProcessResponse(context.Background(), "选A", "用户选A", false)

	calls := waitForFeedback(mock, 1)
	if len(calls) != 1 {
		t.Error("conflict resolution should take priority")
	}
}

func TestPostProcessResponse_RequirementsMode(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	req := makeFullRequirements()
	req.Status = "collecting"
	s.Requirements = req

	p.postProcessResponse(context.Background(), "测试", "好的 [REQUIREMENTS_CONFIRMED]", true)

	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()
	if status != "confirming" {
		t.Errorf("status = %q, want confirming", status)
	}
}

func TestPostProcessResponse_TaskInit(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.postProcessResponse(context.Background(), "帮我做PPT", `好的[TASK_INIT]{"topic":"测试"}[/TASK_INIT]`, false)

	s.reqMu.RLock()
	req := s.Requirements
	s.reqMu.RUnlock()
	if req == nil {
		t.Fatal("Requirements should be created")
	}
}

func TestPostProcessResponse_PPTFeedback(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	s.RegisterTask("t1", "测试")
	s.SetActiveTask("t1")

	p.postProcessResponse(context.Background(), "改字体", `好的[PPT_FEEDBACK]{"action_type":"style","instruction":"改字体"}[/PPT_FEEDBACK]`, false)

	calls := waitForFeedback(mock, 1)
	if len(calls) == 0 {
		t.Error("should trigger PPT feedback")
	}
}

// ===========================================================================
// asyncExtractMemory
// ===========================================================================

func TestAsyncExtractMemory(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.asyncExtractMemory("用户说了什么", "助手回复了什么")
	calls := waitForExtractMem(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1 ExtractMemory call, got %d", len(calls))
	}
	if calls[0].UserID != "user_test" {
		t.Errorf("userID = %q", calls[0].UserID)
	}
	if len(calls[0].Messages) != 2 {
		t.Errorf("expected 2 turns, got %d", len(calls[0].Messages))
	}
}

func TestAsyncExtractMemory_EmptyInput(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.asyncExtractMemory("", "")
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := len(mock.extractMemCalls)
	mock.mu.Unlock()
	if calls != 0 {
		t.Error("should not extract memory for empty input")
	}
}

func TestAsyncExtractMemory_NilClients(t *testing.T) {
	s := newTestSession(nil)
	p := newTestPipeline(s, nil)
	p.asyncExtractMemory("test", "test") // should not panic
}

// ===========================================================================
// asyncQuery
// ===========================================================================

func TestAsyncQuery_Success(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.asyncQuery(context.Background(), "test_source", "test_type", func() (string, error) {
		return "result content", nil
	})

	time.Sleep(50 * time.Millisecond)
	msgs := p.drainContextQueue()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Source != "test_source" {
		t.Errorf("source = %q", msgs[0].Source)
	}
	if msgs[0].MsgType != "test_type" {
		t.Errorf("msg_type = %q", msgs[0].MsgType)
	}
	if msgs[0].Content != "result content" {
		t.Errorf("content = %q", msgs[0].Content)
	}
}

func TestAsyncQuery_EmptyResult(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.asyncQuery(context.Background(), "src", "typ", func() (string, error) {
		return "", nil
	})

	time.Sleep(50 * time.Millisecond)
	msgs := p.drainContextQueue()
	if len(msgs) != 0 {
		t.Error("empty result should not be enqueued")
	}
}

func TestAsyncQuery_FallbackMsgType(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.asyncQuery(context.Background(), "src", "", func() (string, error) {
		return "data", nil
	})

	time.Sleep(50 * time.Millisecond)
	msgs := p.drainContextQueue()
	if len(msgs) != 1 {
		t.Fatal("expected 1 message")
	}
	if msgs[0].MsgType != "tool_result" {
		t.Errorf("fallback msg_type = %q, want tool_result", msgs[0].MsgType)
	}
}

// ===========================================================================
// OnInterrupt
// ===========================================================================

func TestOnInterrupt_PreservesTokens(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.rawGeneratedTokens.WriteString("<think>reasoning</think>visible text")
	p.OnInterrupt()

	if p.rawGeneratedTokens.Len() != 0 {
		t.Error("raw tokens should be cleared after interrupt")
	}
}

func TestOnInterrupt_ClosesUnclosedThink(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.rawGeneratedTokens.WriteString("<think>unclosed reasoning")
	p.OnInterrupt()

	msgs := p.history.ToOpenAI()
	if len(msgs) < 2 {
		t.Fatal("expected at least 2 messages (system + interrupted)")
	}
}

func TestOnInterrupt_EmptyTokens(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.OnInterrupt() // should not panic
}

// ===========================================================================
// OnAudioData / OnVADEnd
// ===========================================================================

func TestOnAudioData_NilChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	p.audioCh = nil
	p.OnAudioData([]byte{1, 2, 3}) // should not panic
}

func TestOnAudioData_WithChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	p.audioCh = make(chan []byte, 10)
	p.OnAudioData([]byte{1, 2, 3})

	select {
	case data := <-p.audioCh:
		if len(data) != 3 {
			t.Errorf("expected 3 bytes, got %d", len(data))
		}
	default:
		t.Error("expected data in audioCh")
	}
}

func TestOnVADEnd_NilChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	p.vadEndCh = nil
	p.OnVADEnd() // should not panic
}

func TestOnVADEnd_WithChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	p.vadEndCh = make(chan struct{}, 1)
	p.OnVADEnd()

	select {
	case <-p.vadEndCh:
	default:
		t.Error("expected signal in vadEndCh")
	}
}

// ===========================================================================
// Draft thinking helpers
// ===========================================================================

func TestDraftOutput_AppendGetReset(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.appendDraftOutput("hello ")
	p.appendDraftOutput("world")
	if got := p.getDraftOutput(); got != "hello world" {
		t.Errorf("got %q", got)
	}
	p.resetDraftOutput()
	if got := p.getDraftOutput(); got != "" {
		t.Errorf("after reset: %q", got)
	}
}

// ===========================================================================
// drainASRResults
// ===========================================================================

func TestDrainASRResults_Normal(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ch := make(chan ASRResult, 3)
	ch <- ASRResult{Text: "hello", Mode: "online"}
	ch <- ASRResult{Text: "final", Mode: "2pass-offline"}
	close(ch)

	var partials []string
	var finalText string
	p.drainASRResults(context.Background(), ch, &partials, &finalText, time.Second)

	if finalText != "final" {
		t.Errorf("finalText = %q", finalText)
	}
	if len(partials) != 1 || partials[0] != "hello" {
		t.Errorf("partials = %v", partials)
	}
}

func TestDrainASRResults_Timeout(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ch := make(chan ASRResult)
	var partials []string
	var finalText string

	done := make(chan struct{})
	go func() {
		p.drainASRResults(context.Background(), ch, &partials, &finalText, 50*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drainASRResults should timeout")
	}
}

func TestDrainASRResults_ContextCancel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ch := make(chan ASRResult)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var partials []string
	var finalText string

	done := make(chan struct{})
	go func() {
		p.drainASRResults(ctx, ch, &partials, &finalText, 5*time.Second)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drainASRResults should exit on cancelled context")
	}
}
