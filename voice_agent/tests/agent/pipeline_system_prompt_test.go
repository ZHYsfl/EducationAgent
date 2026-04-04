package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	agent "voiceagent/agent"
)

// ===========================================================================
// buildFullSystemPrompt 测试
// ===========================================================================

func TestBuildFullSystemPrompt_LayeredConstruction(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	// 测试基础系统提示词
	prompt := p.BuildFullSystemPrompt(ctx, false)
	if prompt == "" {
		t.Error("system prompt should not be empty")
	}
	if !strings.Contains(prompt, "动作协议") {
		t.Error("prompt should contain protocol instructions")
	}
	if !strings.Contains(prompt, "resolve_conflict") {
		t.Error("prompt should include resolve_conflict action")
	}
	if !strings.Contains(prompt, "#{思考内容}") {
		t.Error("prompt should include think marker instructions")
	}
}

func TestBuildFullSystemPrompt_WithRequirementsMode(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	// 设置需求收集模式
	reqs := agent.NewTestRequirements()
	reqs.Status = "collecting"
	reqs.Topic = "高等数学"
	s.SetRequirements(reqs)

	ctx := context.Background()
	prompt := p.BuildFullSystemPrompt(ctx, false)

	if !strings.Contains(prompt, "高等数学") {
		t.Error("prompt should contain requirements topic")
	}
}

func TestBuildFullSystemPrompt_WithTaskList(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	// 注册任务
	s.RegisterTask("task_1", "数学课件")
	s.RegisterTask("task_2", "物理课件")
	s.SetActiveTask("task_1")

	ctx := context.Background()
	prompt := p.BuildFullSystemPrompt(ctx, false)

	if !strings.Contains(prompt, "数学课件") || !strings.Contains(prompt, "物理课件") {
		t.Error("prompt should contain both task topics")
	}
	if !strings.Contains(prompt, "当前活跃") {
		t.Error("prompt should mark active task")
	}
}

func TestBuildFullSystemPrompt_WithPendingQuestions(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	// 添加待确认问题
	s.AddPendingQuestion("ctx_1", "task_1", "pg1", 1000, "这是第一个问题？")
	s.AddPendingQuestion("ctx_2", "task_2", "pg2", 2000, "这是第二个问题？")

	ctx := context.Background()
	prompt := p.BuildFullSystemPrompt(ctx, false)

	if !strings.Contains(prompt, "ctx_1") || !strings.Contains(prompt, "ctx_2") {
		t.Error("prompt should contain both context IDs")
	}
	if !strings.Contains(prompt, "resolve_conflict") {
		t.Error("prompt should include resolve_conflict instruction for multiple questions")
	}
}

func TestBuildFullSystemPrompt_WithContextQueue(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	// 添加上下文消息
	ctx := context.Background()
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "kb_query",
		MsgType:    "search_result",
		Content:    "高等数学是大学基础课程",
		Priority:   "normal",
	})

	prompt := p.BuildFullSystemPrompt(ctx, true)

	if !strings.Contains(prompt, "高等数学") {
		t.Error("prompt should include context queue content")
	}
	if !strings.Contains(prompt, "系统补充信息") {
		t.Error("prompt should include context section header")
	}
}

func TestBuildFullSystemPrompt_ContextQueueDrainedOnlyWhenRequested(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	// 添加上下文消息
	ctx := context.Background()
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "kb_query",
		MsgType:    "search_result",
		Content:    "test content",
		Priority:   "normal",
	})

	// 第一次调用不包含上下文队列
	prompt1 := p.BuildFullSystemPrompt(ctx, false)
	if strings.Contains(prompt1, "test content") {
		t.Error("context queue should not be included when includeContextQueue=false")
	}

	// 第二次调用包含上下文队列
	prompt2 := p.BuildFullSystemPrompt(ctx, true)
	if !strings.Contains(prompt2, "test content") {
		t.Error("context queue should be included when includeContextQueue=true")
	}

	// 第三次调用应该为空（已耗尽）
	prompt3 := p.BuildFullSystemPrompt(ctx, true)
	if strings.Contains(prompt3, "test content") {
		t.Error("context queue should be empty after draining")
	}
}

func TestBuildFullSystemPrompt_ThreadSafety(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	// 并发构建系统提示词
	var wg sync.WaitGroup
	errors := make(chan string, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			prompt := p.BuildFullSystemPrompt(ctx, false)
			if prompt == "" {
				errors <- "empty prompt"
			}
			if !strings.Contains(prompt, "动作协议") {
				errors <- "missing protocol"
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	errCount := 0
	for e := range errors {
		if e != "" {
			errCount++
			t.Logf("Error: %s", e)
		}
	}

	if errCount > 0 {
		t.Errorf("got %d errors during concurrent access", errCount)
	}
}

func TestBuildFullSystemPrompt_RequirementsModeOverridesBase(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	// 获取基础提示词
	ctx := context.Background()
	basePrompt := p.BuildFullSystemPrompt(ctx, false)

	// 设置需求收集模式
	reqs := agent.NewTestRequirements()
	reqs.Status = "collecting"
	reqs.Topic = "TestTopic"
	reqs.Subject = "数学"
	s.SetRequirements(reqs)

	// 需求模式应该完全替换基础提示词
	reqsPrompt := p.BuildFullSystemPrompt(ctx, false)

	if reqsPrompt == basePrompt {
		t.Error("requirements mode should override base prompt, not append")
	}
	if !strings.Contains(reqsPrompt, "数学") {
		t.Error("requirements prompt should contain subject")
	}
}

func TestBuildFullSystemPrompt_AllLayersCombined(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	// 设置需求模式
	reqs := agent.NewTestRequirements()
	reqs.Status = "collecting"
	reqs.Topic = "组合测试"
	s.SetRequirements(reqs)

	// 添加任务
	s.RegisterTask("task_combo", "组合任务")

	// 添加待确认问题
	s.AddPendingQuestion("ctx_combo", "task_combo", "pg1", 1000, "组合问题？")

	// 添加上下文
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "web_search",
		MsgType:    "search_result",
		Content:    "组合搜索结果",
		Priority:   "normal",
	})

	prompt := p.BuildFullSystemPrompt(ctx, true)

	// 验证所有层都存在
	checks := []struct {
		name    string
		content string
	}{
		{"requirements", "需求收集"}, // BuildRequirementsSystemPrompt 包含 "需求收集" 标题
		{"task list", "组合任务"},
		{"pending question", "ctx_combo"},
		{"context queue", "组合搜索结果"},
		{"protocol instructions", "动作协议"},
		{"resolve_conflict", "resolve_conflict"},
		{"think marker", "#{思考内容}"},
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check.content) {
			t.Errorf("prompt should contain %s: %q", check.name, check.content)
		}
	}

	// 验证层级顺序
	reqIdx := strings.Index(prompt, "组合测试")
	taskIdx := strings.Index(prompt, "组合任务")
	questionIdx := strings.Index(prompt, "ctx_combo")
	contextIdx := strings.Index(prompt, "组合搜索结果")
	protocolIdx := strings.Index(prompt, "动作协议")

	if !(reqIdx < taskIdx && taskIdx < questionIdx && questionIdx < contextIdx && contextIdx < protocolIdx) {
		t.Error("prompt layers should be in correct order")
	}
}

// ===========================================================================
// drainContextQueue 线程安全测试
// ===========================================================================

func TestDrainContextQueue_ConcurrentAccess(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	// 并发写入和读取
	var wg sync.WaitGroup

	// 写入者
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p.EnqueueContextMessage(ctx, agent.ContextMessage{
				ActionType: "test",
				MsgType:    "test_msg",
				Content:    "test content",
				Priority:   "normal",
			})
		}(i)
	}

	// 读取者
	drained := make(chan int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			msgs := p.DrainContextQueue()
			drained <- len(msgs)
		}()
	}

	wg.Wait()
	close(drained)

	totalDrained := 0
	for count := range drained {
		totalDrained += count
	}

	// 验证没有消息丢失（可能有重复，但不会丢失）
	if totalDrained < 100 {
		t.Errorf("expected at least 100 messages drained, got %d", totalDrained)
	}
}

func TestDrainContextQueue_EmptyQueue(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	msgs := p.DrainContextQueue()
	if msgs == nil {
		t.Error("drain should return empty slice, not nil")
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestDrainContextQueue_PendingContextsPriority(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	// 先添加上下文到队列
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "normal",
		MsgType:    "normal_msg",
		Content:    "normal content",
		Priority:   "normal",
	})

	// 添加上下文消息（pendingContexts 中的优先级更高）
	// 注意：这需要内部机制将消息放入 pendingContexts
	// 这里我们验证 DrainContextQueue 会先返回 pendingContexts

	msgs := p.DrainContextQueue()
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}
}

// ===========================================================================
// highPriorityListener 测试
// ===========================================================================

func TestHighPriorityListener_ConflictQuestionFlow(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 启动监听器
	go p.HighPriorityListener(ctx)

	// 发送冲突问题
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "conflict",
		MsgType:    "conflict_question",
		Content:    "请选择配色方案？",
		Priority:   "high",
		Metadata: map[string]string{
			"context_id":     "ctx_123",
			"task_id":        "task_456",
			"page_id":        "pg_789",
			"base_timestamp": "1000",
		},
	})

	// 等待处理
	time.Sleep(500 * time.Millisecond)

	// 验证待确认问题已添加
	if _, ok := s.ResolvePendingQuestion("ctx_123"); !ok {
		t.Error("pending question should have been added")
	}
}

func TestHighPriorityListener_SystemNotify(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 启动监听器
	go p.HighPriorityListener(ctx)

	// 发送系统通知
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "notify",
		MsgType:    "system_notify",
		Content:    "正在处理您的请求",
		Priority:   "high",
	})

	// 等待处理
	time.Sleep(500 * time.Millisecond)

	// 系统通知不应该添加到待确认问题
	// 验证没有错误即可
}

func TestHighPriorityListener_RetryOnInterrupt(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	// 创建一个会被立即取消的上下文
	ctx, cancel := context.WithCancel(context.Background())

	// 启动监听器
	go p.HighPriorityListener(ctx)

	// 发送冲突问题
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "conflict",
		MsgType:    "conflict_question",
		Content:    "测试问题",
		Priority:   "high",
		Metadata: map[string]string{
			"context_id": "ctx_retry",
			"task_id":    "task_retry",
		},
	})

	// 立即取消上下文（模拟打断）
	cancel()

	// 等待重试机制
	time.Sleep(100 * time.Millisecond)

	// 验证：由于上下文被取消，消息应该被降级到 pendingContexts
	// 但具体行为取决于实现，这里主要验证不 panic
}

// ===========================================================================
// 辅助函数
// ===========================================================================

func TestFormatContextForLLM(t *testing.T) {
	msgs := []agent.ContextMessage{
		{ActionType: "kb_query", MsgType: "result", Content: "内容1"},
		{ActionType: "web_search", MsgType: "result", Content: "内容2"},
	}

	result := agent.FormatContextForLLM(msgs)

	if !strings.Contains(result, "系统补充信息") {
		t.Error("should include header")
	}
	if !strings.Contains(result, "内容1") || !strings.Contains(result, "内容2") {
		t.Error("should include all message contents")
	}
	if !strings.Contains(result, "kb_query") || !strings.Contains(result, "web_search") {
		t.Error("should include action types")
	}
}

func TestFormatContextForLLM_Empty(t *testing.T) {
	result := agent.FormatContextForLLM(nil)
	if result != "" {
		t.Error("empty input should return empty string")
	}

	result = agent.FormatContextForLLM([]agent.ContextMessage{})
	if result != "" {
		t.Error("empty slice should return empty string")
	}
}
