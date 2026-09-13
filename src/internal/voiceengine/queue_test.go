package voiceengine

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueuePushPop(t *testing.T) {
	q := NewTokenQueue(2)
	ctx := context.Background()

	if err := q.Push(ctx, []Token{{Content: "a", State: StateTTSTokens, Gen: 1}}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := q.Push(ctx, []Token{{Content: "b", State: StateIdle, Gen: 1}}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	batch := <-q.Chan()
	if len(batch) != 1 || batch[0].Content != "a" {
		t.Fatalf("first batch = %+v", batch)
	}
	batch = <-q.Chan()
	if len(batch) != 1 || batch[0].Content != "b" {
		t.Fatalf("second batch = %+v", batch)
	}
}

func TestQueuePushBlocksUntilCancel(t *testing.T) {
	q := NewTokenQueue(1)
	ctx, cancel := context.WithCancel(context.Background())

	if err := q.Push(ctx, []Token{{Content: "a"}}); err != nil {
		t.Fatalf("first Push: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- q.Push(ctx, []Token{{Content: "b"}})
	}()

	select {
	case err := <-errCh:
		t.Fatalf("Push returned before cancel: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Push error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Push did not return after cancel")
	}
}

func TestQueueDefaultCapacity(t *testing.T) {
	q := NewTokenQueue(0)
	ctx := context.Background()

	for i := 0; i < defaultQueueCapacity; i++ {
		if err := q.Push(ctx, []Token{{Content: "x"}}); err != nil {
			t.Fatalf("Push %d: %v", i, err)
		}
	}

	blocked := make(chan struct{})
	go func() {
		q.Push(ctx, []Token{{Content: "overflow"}})
		close(blocked)
	}()

	select {
	case <-blocked:
		t.Fatal("Push beyond capacity must block")
	case <-time.After(100 * time.Millisecond):
	}
}
