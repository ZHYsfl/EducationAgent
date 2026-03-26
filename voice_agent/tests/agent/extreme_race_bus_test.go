package agent_test

import (
	"sync"
	"testing"

	agent "voiceagent/agent"
)

// TestPipeline_ConcurrentContextBusOperations 测试 context bus 的并发操作
func TestPipeline_ConcurrentContextBusOperations(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})

	for round := 0; round < 10; round++ {
		var wg sync.WaitGroup

		// 并发添加 pending questions
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				s.AddPendingQuestion("ctx_"+string(rune('A'+id%10)), "question")
			}(i)
		}

		// 并发读取 pending questions
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = s.GetPendingQuestions()
			}()
		}

		wg.Wait()
	}
}
