package agent_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agent "voiceagent/agent"
)

// ===========================================================================
// 高优先级队列竞态测试
// ===========================================================================

func TestHighPriorityQueue_ConcurrentEnqueueDequeue(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipelineWithTTS(s, mock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 启动监听器
	go p.HighPriorityListener(ctx)

	var wg sync.WaitGroup
	enqueued := int32(0)

	// 并发写入
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p.EnqueueContextMessage(ctx, agent.ContextMessage{
				ActionType:  "test",
				MsgType:    "system_notify",
				Content:    "test",
				Priority:   "high",
			})
			atomic.AddInt32(&enqueued, 1)
		}(i)
	}

	// 等待写入完成
	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	// 验证没有 panic
	cancel()

	t.Logf("Enqueued: %d", atomic.LoadInt32(&enqueued))
}

func TestContextQueue_ConcurrentDrainAndEnqueue(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	stop := make(chan bool)

	// 持续写入
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			counter := 0
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					return
				default:
					p.EnqueueContextMessage(ctx, agent.ContextMessage{
						ActionType:  "writer",
						MsgType:    "test",
						Content:    "content",
						Priority:   "normal",
					})
					counter++
					if counter%100 == 0 {
						time.Sleep(time.Millisecond)
					}
				}
			}
		}(i)
	}

	// 持续读取
	drained := int32(0)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					return
				default:
					msgs := p.DrainContextQueue()
					if len(msgs) > 0 {
						atomic.AddInt32(&drained, int32(len(msgs)))
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// 运行一段时间
	time.Sleep(500 * time.Millisecond)
	close(stop)
	cancel() // 取消context，解除阻塞的goroutines
	wg.Wait()

	t.Logf("Total drained: %d", atomic.LoadInt32(&drained))
}

// ===========================================================================
// Session 状态竞态测试
// ===========================================================================

func TestSession_ConcurrentStateAccess(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)

	var wg sync.WaitGroup
	errors := make(chan string, 100)

	// 并发读写状态
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				s.SetState(agent.StateProcessing)
				state := s.GetState()
				// SessionState 有 String() 方法
				if state.String() == "" || state.String() == "unknown" {
					errors <- "empty state"
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errCount := 0
	for e := range errors {
		if e != "" {
			errCount++
		}
	}

	if errCount > 0 {
		t.Errorf("got %d errors during concurrent state access", errCount)
	}
}

// ===========================================================================
// Pipeline 竞态测试
// ===========================================================================

func TestPipeline_ConcurrentSystemPromptBuild(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// 并发构建系统提示词
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				prompt := p.BuildFullSystemPrompt(ctx, false)
				if prompt == "" {
					errors <- nil // 使用 nil 表示空提示词错误
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errCount := 0
	for e := range errors {
		if e == nil {
			errCount++
		}
	}

	if errCount > 0 {
		t.Errorf("got %d empty prompts", errCount)
	}
}

func TestPipeline_ConcurrentDrainAndEnqueue(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	var wg sync.WaitGroup
	stop := make(chan bool)

	// 持续写入上下文队列
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					p.EnqueueContextMessage(ctx, agent.ContextMessage{
						ActionType:  "concurrent_test",
						MsgType:    "test",
						Content:    "test content",
						Priority:   "normal",
					})
				}
			}
		}(i)
	}

	// 持续读取上下文队列
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = p.DrainContextQueue()
				}
			}
		}(i)
	}

	// 运行一段时间后停止
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// ===========================================================================
// 混合竞态场景
// ===========================================================================

func TestPipeline_FullChaos(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// 启动监听器
	go p.HighPriorityListener(ctx)

	// 各种并发操作
	operations := []func(){
		func() { p.BuildFullSystemPrompt(ctx, false) },
		func() { p.DrainContextQueue() },
		func() {
			p.EnqueueContextMessage(ctx, agent.ContextMessage{
				ActionType:  "test",
				MsgType:    "test",
				Content:    "test",
				Priority:   "normal",
			})
		},
		func() {
			p.EnqueueContextMessage(ctx, agent.ContextMessage{
				ActionType:  "test",
				MsgType:    "system_notify",
				Content:    "notify",
				Priority:   "high",
			})
		},
		func() { s.RegisterTask("task_"+string(rune('a'+time.Now().UnixNano()%26)), "topic") },
		func() { s.SetActiveTask("task_a") },
		func() { s.AddPendingQuestion("ctx_"+string(rune('a'+time.Now().UnixNano()%26)), "task_a", "pg1", 0, "q") },
		func() { _, _ = s.ResolvePendingQuestion("ctx_a") },
		func() { s.SetState(agent.StateProcessing) },
		func() { _ = s.GetState() },
	}

	// 启动多个 goroutine 随机执行操作
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					op := operations[id%len(operations)]
					op()
					time.Sleep(time.Microsecond * 100)
				}
			}
		}(i)
	}

	// 等待超时
	<-ctx.Done()
	wg.Wait()

	// 没有 panic 即成功
}

// ===========================================================================
// 内存泄漏测试
// ===========================================================================

func TestContextQueue_MemoryLeak(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	// 大量消息写入
	for i := 0; i < 10000; i++ {
		p.EnqueueContextMessage(ctx, agent.ContextMessage{
			ActionType:  "memory_test",
			MsgType:    "test",
			Content:    "test content " + string(rune('a'+i%26)),
			Priority:   "normal",
		})
	}

	// 全部读取
	msgs := p.DrainContextQueue()
	t.Logf("Drained %d messages", len(msgs))

	// 再次写入并读取
	for i := 0; i < 1000; i++ {
		p.EnqueueContextMessage(ctx, agent.ContextMessage{
			ActionType:  "memory_test",
			MsgType:    "test",
			Content:    "test content 2",
			Priority:   "normal",
		})
	}

	msgs2 := p.DrainContextQueue()
	t.Logf("Drained %d messages (second batch)", len(msgs2))

	if len(msgs2) != 1000 {
		t.Errorf("expected 1000 messages in second batch, got %d", len(msgs2))
	}
}

func TestPendingQuestions_MemoryLeak(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)

	// 添加大量问题
	for i := 0; i < 1000; i++ {
		ctxID := "ctx_" + string(rune('a'+i%26)) + "_" + string(rune('0'+i/26))
		s.AddPendingQuestion(ctxID, "task_a", "pg1", int64(i), "question")
	}

	// 解析所有问题
	// 由于 GetAllPendingQuestions 不存在，我们通过 ResolvePendingQuestion 来检查
	resolved := 0
	for i := 0; i < 1000; i++ {
		ctxID := "ctx_" + string(rune('a'+i%26)) + "_" + string(rune('0'+i/26))
		if _, ok := s.ResolvePendingQuestion(ctxID); ok {
			resolved++
		}
	}

	t.Logf("Resolved: %d", resolved)

	// 验证清空 - 再次解析应该都失败
	remaining := 0
	for i := 0; i < 1000; i++ {
		ctxID := "ctx_" + string(rune('a'+i%26)) + "_" + string(rune('0'+i/26))
		if _, ok := s.ResolvePendingQuestion(ctxID); ok {
			remaining++
		}
	}

	if remaining != 0 {
		t.Errorf("expected 0 remaining questions, got %d", remaining)
	}
}
