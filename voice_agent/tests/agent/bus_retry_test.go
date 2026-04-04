package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"testing"
	"time"
)

type cancelAwareTTS struct{}

func (c *cancelAwareTTS) Synthesize(ctx context.Context, text string, bufSize int) (<-chan []byte, error) {
	ch := make(chan []byte)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func TestHighPriorityListener_Interrupted_Requeue(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewTestPipeline(s, s.GetClients())
	p.SetTTSClient(&cancelAwareTTS{})

	ctx, cancel := context.WithCancel(context.Background())
	go p.HighPriorityListener(ctx)

	p.GetHighPriorityQueue() <- agent.ContextMessage{
		MsgType: "conflict_question",
		Content: "请确认选择A还是B？",
		Metadata: map[string]string{
			"task_id":    "task_1",
			"context_id": "ctx_1",
			"_retries":   "1",
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	select {
	case msg := <-p.GetHighPriorityQueue():
		if msg.Metadata["_retries"] != "2" {
			t.Fatalf("retries should increment to 2, got %q", msg.Metadata["_retries"])
		}
	default:
		t.Fatal("expected requeued conflict question")
	}
}

func TestHighPriorityListener_Interrupted_DemoteAfterMaxRetry(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewTestPipeline(s, s.GetClients())
	p.SetTTSClient(&cancelAwareTTS{})

	ctx, cancel := context.WithCancel(context.Background())
	go p.HighPriorityListener(ctx)

	p.GetHighPriorityQueue() <- agent.ContextMessage{
		MsgType: "conflict_question",
		Content: "再次确认问题",
		Metadata: map[string]string{
			"task_id":    "task_2",
			"context_id": "ctx_2",
			"_retries":   "2",
		},
	}

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	p.LockPending()
	defer p.UnlockPending()
	if len(p.GetPendingContexts()) == 0 {
		t.Fatal("expected conflict question to be demoted to pending context")
	}
}
