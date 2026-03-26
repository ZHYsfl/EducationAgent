package executor

import (
	"sync"
	"testing"

	"voiceagent/internal/bus"
	"voiceagent/internal/protocol"
	"voiceagent/internal/types"
)

func TestExecutorRaceCondition(t *testing.T) {
	b := bus.New()
	exec := New(b, &mockClients{})

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []types.ContextMessage{}

	callback := func(msg types.ContextMessage) {
		mu.Lock()
		results = append(results, msg)
		mu.Unlock()
		wg.Done()
	}

	n := 100
	wg.Add(n)

	for i := 0; i < n; i++ {
		go exec.Execute(protocol.Action{
			Type:   "ppt_init",
			Params: map[string]string{"topic": "AI"},
		}, callback)
	}

	wg.Wait()

	if len(results) != n {
		t.Errorf("expected %d results, got %d", n, len(results))
	}
}
