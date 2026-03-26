package executor

import (
	"context"
	"sync"
	"testing"
	"time"

	"voiceagent/internal/bus"
	"voiceagent/internal/protocol"
	"voiceagent/internal/types"
)

type mockClients struct {
	failInit     bool
	failFeedback bool
	failKB       bool
}

func (m *mockClients) InitPPT(ctx context.Context, req types.PPTInitRequest) (types.PPTInitResponse, error) {
	time.Sleep(10 * time.Millisecond)
	if m.failInit {
		return types.PPTInitResponse{}, context.DeadlineExceeded
	}
	return types.PPTInitResponse{TaskID: "test_task"}, nil
}

func (m *mockClients) SendFeedback(ctx context.Context, req types.PPTFeedbackRequest) error {
	time.Sleep(10 * time.Millisecond)
	if m.failFeedback {
		return context.DeadlineExceeded
	}
	return nil
}

func (m *mockClients) QueryKB(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error) {
	time.Sleep(10 * time.Millisecond)
	if m.failKB {
		return types.KBQueryResponse{}, context.DeadlineExceeded
	}
	return types.KBQueryResponse{}, nil
}

func TestExecutorConcurrent(t *testing.T) {
	b := bus.New()
	exec := New(b, &mockClients{})

	var wg sync.WaitGroup
	results := make(chan types.ContextMessage, 100)

	callback := func(msg types.ContextMessage) {
		results <- msg
		wg.Done()
	}

	actions := []protocol.Action{
		{Type: "ppt_init", Params: map[string]string{"topic": "AI"}},
		{Type: "ppt_mod", Params: map[string]string{"task": "t1", "page": "p1", "action": "modify", "ins": "test"}},
		{Type: "kb_query", Params: map[string]string{"q": "test"}},
	}

	wg.Add(len(actions))
	for _, action := range actions {
		exec.Execute(action, callback)
	}

	wg.Wait()
	close(results)

	count := 0
	for range results {
		count++
	}

	if count != 3 {
		t.Errorf("expected 3 results, got %d", count)
	}
}

func TestExecutorErrors(t *testing.T) {
	b := bus.New()
	exec := New(b, &mockClients{failInit: true, failFeedback: true, failKB: true})

	var wg sync.WaitGroup
	results := make(chan types.ContextMessage, 10)

	callback := func(msg types.ContextMessage) {
		results <- msg
		wg.Done()
	}

	actions := []protocol.Action{
		{Type: "ppt_init", Params: map[string]string{"topic": "AI"}},
		{Type: "ppt_mod", Params: map[string]string{"task": "t1", "page": "p1"}},
		{Type: "kb_query", Params: map[string]string{"q": "test"}},
	}

	wg.Add(len(actions))
	for _, action := range actions {
		exec.Execute(action, callback)
	}

	wg.Wait()
	close(results)

	for msg := range results {
		if msg.Content == "" {
			t.Error("expected error message")
		}
	}
}

func TestExecutorNilClients(t *testing.T) {
	b := bus.New()
	exec := New(b, nil)

	var wg sync.WaitGroup
	results := make(chan types.ContextMessage, 10)

	callback := func(msg types.ContextMessage) {
		results <- msg
		wg.Done()
	}

	actions := []protocol.Action{
		{Type: "ppt_init", Params: map[string]string{"topic": "AI"}},
		{Type: "ppt_mod", Params: map[string]string{"task": "t1"}},
		{Type: "kb_query", Params: map[string]string{"q": "test"}},
	}

	wg.Add(len(actions))
	for _, action := range actions {
		exec.Execute(action, callback)
	}

	wg.Wait()
	close(results)

	for msg := range results {
		if msg.Content == "" {
			t.Error("expected error message")
		}
	}
}

func TestExecutorUnknownAction(t *testing.T) {
	b := bus.New()
	exec := New(b, &mockClients{})

	done := make(chan types.ContextMessage, 1)
	exec.Execute(protocol.Action{Type: "unknown"}, func(msg types.ContextMessage) {
		done <- msg
	})

	msg := <-done
	if msg.Content == "" {
		t.Error("expected unknown action message")
	}
}

func TestExecutorHighPriority(t *testing.T) {
	b := bus.New()
	exec := New(b, &mockClients{})

	done := make(chan types.ContextMessage, 1)
	exec.Execute(protocol.Action{
		Type:   "ppt_init",
		Params: map[string]string{"topic": "AI", "p": "h"},
	}, func(msg types.ContextMessage) {
		done <- msg
	})

	msg := <-done
	if msg.Priority != "high" {
		t.Errorf("expected high priority, got %s", msg.Priority)
	}
}
