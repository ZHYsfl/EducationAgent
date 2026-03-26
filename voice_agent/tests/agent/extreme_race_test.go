package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	agent "voiceagent/agent"
)

// TestPipeline_ConcurrentTokenWriteAndCancel 测试 token 写入和取消的竞态
func TestPipeline_ConcurrentTokenWriteAndCancel(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewPipeline(s, s.GetConfig(), s.GetClients())
	s.SetPipeline(p)

	for round := 0; round < 10; round++ {
		ctx, cancel := context.WithCancel(context.Background())

		var wg sync.WaitGroup
		// 持续写入 token
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				select {
				case <-ctx.Done():
					return
				default:
					p.AppendRawToken("x")
				}
			}
		}()

		// 并发调用 OnInterrupt
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(5 * time.Millisecond)
			p.OnInterrupt()
		}()

		cancel()
		wg.Wait()
	}
}

// TestSession_ConcurrentStateChangeAndPipelineCancel 测试状态切换和 pipeline 取消的竞态
func TestSession_ConcurrentStateChangeAndPipelineCancel(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewPipeline(s, s.GetConfig(), s.GetClients())
	s.SetPipeline(p)

	for round := 0; round < 20; round++ {
		var wg sync.WaitGroup

		// 并发切换状态
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				states := []agent.SessionState{
					agent.StateIdle,
					agent.StateListening,
					agent.StateProcessing,
					agent.StateSpeaking,
				}
				s.SetState(states[id%4])
			}(i)
		}

		// 并发取消 pipeline
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.CancelCurrentPipeline()
			}()
		}

		wg.Wait()
	}
}

// TestSession_ConcurrentTaskOperations 测试任务注册和查询的竞态
func TestSession_ConcurrentTaskOperations(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})

	for round := 0; round < 10; round++ {
		var wg sync.WaitGroup

		// 并发注册任务
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				taskID := agent.NewID("task")
				s.RegisterTask(taskID, "test_topic")
			}(i)
		}

		// 并发设置 active task
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.SetActiveTask("task_x")
			}()
		}

		// 并发获取任务列表
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = s.GetAllTasks()
			}()
		}

		wg.Wait()
	}
}
