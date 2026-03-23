package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"testing"
	"time"
)

// ===========================================================================
// drainContextQueue
// ===========================================================================

func TestDrainContextQueue_EmptyQueue(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	msgs := p.DrainContextQueue()
	if len(msgs) != 0 {
		t.Errorf("expected 0, got %d", len(msgs))
	}
}

func TestDrainContextQueue_FromChannel(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.GetContextQueue() <- agent.ContextMessage{ID: "c1", Content: "msg1"}
	p.GetContextQueue() <- agent.ContextMessage{ID: "c2", Content: "msg2"}

	msgs := p.DrainContextQueue()
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].ID != "c1" || msgs[1].ID != "c2" {
		t.Error("wrong order or content")
	}
}

func TestDrainContextQueue_FromPending(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.SetPendingContexts([]agent.ContextMessage{
		{ID: "p1", Content: "pending1"},
	})
	p.GetContextQueue() <- agent.ContextMessage{ID: "c1", Content: "channel1"}

	msgs := p.DrainContextQueue()
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].ID != "p1" {
		t.Error("pending messages should come first")
	}
	if len(p.GetPendingContexts()) != 0 {
		t.Error("pending should be cleared")
	}
}

// ===========================================================================
// enqueueContextMessage
// ===========================================================================

func TestEnqueueContextMessage_Normal(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.EnqueueContextMessage(context.Background(), agent.ContextMessage{
		ID:       "n1",
		Priority: "normal",
	})

	select {
	case msg := <-p.GetContextQueue():
		if msg.ID != "n1" {
			t.Error("wrong message in normal queue")
		}
	default:
		t.Error("expected message in contextQueue")
	}
}

func TestEnqueueContextMessage_High(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.EnqueueContextMessage(context.Background(), agent.ContextMessage{
		ID:       "h1",
		Priority: "high",
	})

	select {
	case msg := <-p.GetHighPriorityQueue():
		if msg.ID != "h1" {
			t.Error("wrong message in high priority queue")
		}
	default:
		t.Error("expected message in highPriorityQueue")
	}
}

func TestEnqueueContextMessage_CancelledContext(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Fill both queues
	for i := 0; i < cap(p.GetContextQueue()); i++ {
		p.GetContextQueue() <- agent.ContextMessage{ID: "filler"}
	}
	for i := 0; i < cap(p.GetHighPriorityQueue()); i++ {
		p.GetHighPriorityQueue() <- agent.ContextMessage{ID: "filler"}
	}

	// Should not block with cancelled context
	done := make(chan struct{})
	go func() {
		p.EnqueueContextMessage(ctx, agent.ContextMessage{ID: "overflow", Priority: "normal"})
		p.EnqueueContextMessage(ctx, agent.ContextMessage{ID: "overflow", Priority: "high"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enqueueContextMessage blocked on full queue with cancelled context")
	}
}

// ===========================================================================
// asyncQuery
// ===========================================================================

func TestAsyncQuery_Success(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.AsyncQuery(context.Background(), "test_source", "test_type", func() (string, error) {
		return "result content", nil
	})

	time.Sleep(50 * time.Millisecond)
	msgs := p.DrainContextQueue()
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
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.AsyncQuery(context.Background(), "src", "typ", func() (string, error) {
		return "", nil
	})

	time.Sleep(50 * time.Millisecond)
	msgs := p.DrainContextQueue()
	if len(msgs) != 0 {
		t.Error("empty result should not be enqueued")
	}
}

func TestAsyncQuery_FallbackMsgType(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.AsyncQuery(context.Background(), "src", "", func() (string, error) {
		return "data", nil
	})

	time.Sleep(50 * time.Millisecond)
	msgs := p.DrainContextQueue()
	if len(msgs) != 1 {
		t.Fatal("expected 1 message")
	}
	if msgs[0].MsgType != "tool_result" {
		t.Errorf("fallback msg_type = %q, want tool_result", msgs[0].MsgType)
	}
}
