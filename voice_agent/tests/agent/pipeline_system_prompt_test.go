package agent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	agent "voiceagent/agent"
)

func TestBuildFullSystemPrompt_LayeredConstruction(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	prompt := p.BuildFullSystemPrompt(context.Background(), false)
	if prompt == "" {
		t.Fatal("system prompt should not be empty")
	}
	if !strings.Contains(prompt, "动作协议") {
		t.Error("prompt should contain protocol instructions")
	}
	if !strings.Contains(prompt, "#{思考内容}") {
		t.Error("prompt should include think marker instructions")
	}
}

func TestBuildFullSystemPrompt_WithRequirementsMode(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	reqs := agent.NewTestRequirements()
	reqs.Status = "collecting"
	reqs.Topic = "数学课件"
	s.SetRequirements(reqs)

	prompt := p.BuildFullSystemPrompt(context.Background(), false)
	if !strings.Contains(prompt, "数学课件") {
		t.Error("prompt should contain requirements topic")
	}
}

func TestBuildFullSystemPrompt_WithTaskList(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.RegisterTask("task_1", "数学课件")
	s.RegisterTask("task_2", "物理课件")
	s.SetActiveTask("task_1")

	prompt := p.BuildFullSystemPrompt(context.Background(), false)
	if !strings.Contains(prompt, "数学课件") || !strings.Contains(prompt, "物理课件") {
		t.Error("prompt should contain both task topics")
	}
	if !strings.Contains(prompt, "当前任务") {
		t.Error("prompt should mark active task")
	}
}

func TestBuildFullSystemPrompt_WithPendingQuestions(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	s.AddPendingQuestion("ctx_1", "task_1", "pg1", 1000, "问题1")
	s.AddPendingQuestion("ctx_2", "task_2", "pg2", 2000, "问题2")

	prompt := p.BuildFullSystemPrompt(context.Background(), false)
	if !strings.Contains(prompt, "ctx_1") || !strings.Contains(prompt, "ctx_2") {
		t.Error("prompt should contain both context IDs")
	}
	if !strings.Contains(prompt, "resolve_conflict") {
		t.Error("prompt should include resolve_conflict instruction")
	}
}

func TestBuildFullSystemPrompt_WithContextQueue(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "kb_query",
		MsgType:    "search_result",
		Content:    "数学检索结果",
		Priority:   "normal",
	})

	prompt := p.BuildFullSystemPrompt(ctx, true)
	if !strings.Contains(prompt, "数学检索结果") {
		t.Error("prompt should include context queue content")
	}
	if !strings.Contains(prompt, "tool_context") {
		t.Error("prompt should include context section header")
	}
}

func TestBuildFullSystemPrompt_ContextQueueDrainedOnlyWhenRequested(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "kb_query",
		MsgType:    "search_result",
		Content:    "test content",
		Priority:   "normal",
	})

	prompt1 := p.BuildFullSystemPrompt(ctx, false)
	if strings.Contains(prompt1, "test content") {
		t.Error("context queue should not be included when includeContextQueue=false")
	}

	prompt2 := p.BuildFullSystemPrompt(ctx, true)
	if !strings.Contains(prompt2, "test content") {
		t.Error("context queue should be included when includeContextQueue=true")
	}

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
	var wg sync.WaitGroup
	errors := make(chan string, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prompt := p.BuildFullSystemPrompt(ctx, false)
			if prompt == "" {
				errors <- "empty prompt"
			}
			if !strings.Contains(prompt, "动作协议") {
				errors <- "missing protocol"
			}
		}()
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
		t.Errorf("got %d errors during concurrent access", errCount)
	}
}

func TestBuildFullSystemPrompt_RequirementsModeOverridesBase(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()
	basePrompt := p.BuildFullSystemPrompt(ctx, false)

	reqs := agent.NewTestRequirements()
	reqs.Status = "collecting"
	reqs.Topic = "TestTopic"
	reqs.Subject = "数学"
	s.SetRequirements(reqs)

	reqsPrompt := p.BuildFullSystemPrompt(ctx, false)
	if reqsPrompt == basePrompt {
		t.Error("requirements mode should override base prompt, not append")
	}
	if !strings.Contains(reqsPrompt, "TestTopic") {
		t.Error("requirements prompt should contain topic")
	}
}

func TestBuildFullSystemPrompt_AllLayersCombined(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()

	reqs := agent.NewTestRequirements()
	reqs.Status = "collecting"
	reqs.Topic = "组合测试"
	s.SetRequirements(reqs)

	s.RegisterTask("task_combo", "组合任务")
	s.AddPendingQuestion("ctx_combo", "task_combo", "pg1", 1000, "组合冲突问题")
	p.EnqueueContextMessage(ctx, agent.ContextMessage{
		ActionType: "web_search",
		MsgType:    "search_result",
		Content:    "组合搜索结果",
		Priority:   "normal",
	})

	prompt := p.BuildFullSystemPrompt(ctx, true)
	checks := []string{"组合测试", "组合任务", "ctx_combo", "组合搜索结果", "动作协议", "#{思考内容}"}
	for _, c := range checks {
		if !strings.Contains(prompt, c) {
			t.Errorf("prompt should contain %q", c)
		}
	}

	reqIdx := strings.Index(prompt, "组合测试")
	taskIdx := strings.Index(prompt, "组合任务")
	questionIdx := strings.Index(prompt, "ctx_combo")
	contextIdx := strings.Index(prompt, "组合搜索结果")
	protocolIdx := strings.Index(prompt, "动作协议")
	if !(reqIdx < taskIdx && taskIdx < questionIdx && questionIdx < contextIdx && contextIdx < protocolIdx) {
		t.Error("prompt layers should be in correct order")
	}
}

func TestDrainContextQueue_ConcurrentAccess(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.EnqueueContextMessage(ctx, agent.ContextMessage{
				ActionType: "test",
				MsgType:    "test_msg",
				Content:    "test content",
				Priority:   "normal",
			})
		}()
	}

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

	total := 0
	for n := range drained {
		total += n
	}
	if total < 64 {
		t.Errorf("expected at least 64 messages drained (queue capacity), got %d", total)
	}
}

func TestDrainContextQueue_EmptyQueue(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)
	if len(p.DrainContextQueue()) != 0 {
		t.Error("empty queue should return empty slice")
	}
}

func TestFormatContextForLLM(t *testing.T) {
	msgs := []agent.ContextMessage{
		{ActionType: "kb_query", MsgType: "result", Content: "内容1"},
		{ActionType: "web_search", MsgType: "result", Content: "内容2"},
	}
	result := agent.FormatContextForLLM(msgs)
	if !strings.Contains(result, "tool_context") {
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
	if agent.FormatContextForLLM(nil) != "" {
		t.Error("empty input should return empty string")
	}
	if agent.FormatContextForLLM([]agent.ContextMessage{}) != "" {
		t.Error("empty slice should return empty string")
	}
}
