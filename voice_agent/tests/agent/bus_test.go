package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"testing"
	"time"
)

// ===========================================================================
// highPriorityListener - conflict_question flow
// ===========================================================================

func TestHighPriorityListener_ConflictQuestion(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipelineWithTTS(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.HighPriorityListener(ctx)

	p.GetHighPriorityQueue() <- agent.ContextMessage{
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

	msgs := agent.DrainWriteCh(s)
	found, ok := agent.FindWSMessage(msgs, "conflict_ask")
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
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.HighPriorityListener(ctx)

	p.GetHighPriorityQueue() <- agent.ContextMessage{
		MsgType:  "other_type",
		Content:  "some info",
		Priority: "high",
	}

	time.Sleep(100 * time.Millisecond)

	p.LockPending()
	count := len(p.GetPendingContexts())
	p.UnlockPending()

	if count != 1 {
		t.Errorf("expected 1 pending context, got %d", count)
	}
}

func TestHighPriorityListener_ContextDone(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	done := make(chan struct{})
	go func() {
		p.HighPriorityListener(ctx)
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
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())

	// Fill queue to capacity
	for i := 0; i < cap(p.GetContextQueue()); i++ {
		p.GetContextQueue() <- agent.ContextMessage{ID: "filler"}
	}
	cancel()

	p.AsyncQuery(ctx, "src", "typ", func() (string, error) {
		return "data", nil
	})

	time.Sleep(50 * time.Millisecond)
	// Should not hang - the result is dropped due to full queue + cancelled ctx
}

func TestAsyncQuery_Error(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.AsyncQuery(context.Background(), "src", "typ", func() (string, error) {
		return "", context.Canceled
	})

	time.Sleep(50 * time.Millisecond)
	msgs := p.DrainContextQueue()
	if len(msgs) != 0 {
		t.Error("error should not enqueue message")
	}
}
