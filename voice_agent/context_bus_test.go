package main

import (
	"context"
	"testing"
	"time"
)

// ===========================================================================
// highPriorityListener - conflict_question flow
// ===========================================================================

func TestHighPriorityListener_ConflictQuestion(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipelineWithTTS(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.highPriorityListener(ctx)

	p.highPriorityQueue <- ContextMessage{
		MsgType:  "conflict_question",
		Content:  "选择哪种方案？",
		Priority: "high",
		Metadata: map[string]string{
			"task_id":    "task_hpl",
			"page_id":    "pg1",
			"context_id": "ctx_hpl_1",
		},
	}

	time.Sleep(200 * time.Millisecond)

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "conflict_ask")
	if !ok {
		t.Fatal("expected conflict_ask WS message")
	}
	if found.ContextID != "ctx_hpl_1" {
		t.Errorf("context_id = %q", found.ContextID)
	}
	if found.Question != "选择哪种方案？" {
		t.Errorf("question = %q", found.Question)
	}

	_, ok = s.ResolvePendingQuestion("ctx_hpl_1")
	if !ok {
		t.Error("should have added pending question")
	}
}

func TestHighPriorityListener_NonConflictGoesToPending(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.highPriorityListener(ctx)

	p.highPriorityQueue <- ContextMessage{
		MsgType:  "other_type",
		Content:  "some info",
		Priority: "high",
	}

	time.Sleep(100 * time.Millisecond)

	p.pendingMu.Lock()
	count := len(p.pendingContexts)
	p.pendingMu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 pending context, got %d", count)
	}
}

func TestHighPriorityListener_ContextDone(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	done := make(chan struct{})
	go func() {
		p.highPriorityListener(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("highPriorityListener should exit on cancelled context")
	}
}

// ===========================================================================
// asyncQuery with cancelled context
// ===========================================================================

func TestAsyncQuery_CancelledContext(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())

	// Fill queue to capacity
	for i := 0; i < cap(p.contextQueue); i++ {
		p.contextQueue <- ContextMessage{ID: "filler"}
	}
	cancel()

	p.asyncQuery(ctx, "src", "typ", func() (string, error) {
		return "data", nil
	})

	time.Sleep(50 * time.Millisecond)
	// Should not hang - the result is dropped due to full queue + cancelled ctx
}

func TestAsyncQuery_Error(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.asyncQuery(context.Background(), "src", "typ", func() (string, error) {
		return "", context.Canceled
	})

	time.Sleep(50 * time.Millisecond)
	msgs := p.drainContextQueue()
	if len(msgs) != 0 {
		t.Error("error should not enqueue message")
	}
}
