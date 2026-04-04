package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	agent "voiceagent/agent"
)

// TestOnInterrupt_RaceCondition 测试 OnInterrupt 在流式处理期间的竞态条件
func TestOnInterrupt_RaceCondition(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewPipeline(s, s.GetConfig(), s.GetClients())
	s.SetPipeline(p)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 模拟持续写入 token 的流式处理
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			select {
			case <-ctx.Done():
				return
			default:
				p.WriteRawTokens("token")
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// 在流式处理期间调用 OnInterrupt
	time.Sleep(10 * time.Millisecond)
	p.OnInterrupt()

	cancel()
	wg.Wait()

	// 验证没有 token 丢失（所有写入的 token 都应该被保存）
	// 这个测试主要是检测 race detector 是否报错
}
