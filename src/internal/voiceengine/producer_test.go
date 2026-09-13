package voiceengine

import (
	"context"
	"sync"
	"testing"
	"time"

	"agent_runtime"
	"educationagent/internal/llm"

	"github.com/openai/openai-go/v3"
)

type fakeStreamSource struct {
	fn func(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) <-chan llm.Event
}

func (f *fakeStreamSource) Stream(
	ctx context.Context,
	messages []openai.ChatCompletionMessageParamUnion,
	tools []openai.ChatCompletionToolUnionParam,
) <-chan llm.Event {
	return f.fn(ctx, messages, tools)
}

// tokenCollector drains the engine queue until the idle token shows up.
type tokenCollector struct {
	mu      sync.Mutex
	batches [][]Token
	done    chan struct{}
}

func newTokenCollector(e *Engine) *tokenCollector {
	c := &tokenCollector{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for batch := range e.Queue().Chan() {
			c.mu.Lock()
			c.batches = append(c.batches, batch)
			c.mu.Unlock()
			if batch[len(batch)-1].State == StateIdle {
				return
			}
		}
	}()
	return c
}

func (c *tokenCollector) wait(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for the idle token")
	}
}

func (c *tokenCollector) all() []Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Token
	for _, b := range c.batches {
		out = append(out, b...)
	}
	return out
}

func waitDone(t *testing.T, done <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("producer did not finish in time")
	}
}

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func assertStamped(t *testing.T, tokens []Token, gen int64) {
	t.Helper()
	for _, tok := range tokens {
		if tok.Gen != gen {
			t.Errorf("token %+v stamped with gen %d, want %d", tok, tok.Gen, gen)
		}
	}
}

func TestProducerHappyPath(t *testing.T) {
	e := NewEngine(context.Background())
	e.BumpGeneration()
	gen := e.Generation()

	var mu sync.Mutex
	var toolArgs []map[string]any
	agent := agent_runtime.NewAgent(&agent_runtime.LLMConfig{}, []*agent_runtime.Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
			Func: func(ctx context.Context, args map[string]any) (string, error) {
				mu.Lock()
				toolArgs = append(toolArgs, args)
				mu.Unlock()
				return "晴", nil
			},
			Parameters: map[string]any{
				"type":     "object",
				"required": []any{"city"},
			},
		},
	})

	var round2Messages []openai.ChatCompletionMessageParamUnion
	round := 0
	source := &fakeStreamSource{}
	source.fn = func(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) <-chan llm.Event {
		round++
		ch := make(chan llm.Event, 8)
		go func() {
			defer close(ch)
			switch round {
			case 1:
				ch <- llm.Event{Kind: llm.EventContent, Text: "好的，"}
				ch <- llm.Event{Kind: llm.EventToolCall, ToolCall: llm.ToolCallPart{Index: 0, ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`}}
				ch <- llm.Event{Kind: llm.EventFinish, FinishReason: "tool_calls"}
			case 2:
				round2Messages = messages
				ch <- llm.Event{Kind: llm.EventContent, Text: "北京今天晴。"}
				ch <- llm.Event{Kind: llm.EventFinish, FinishReason: "stop"}
			default:
				t.Errorf("unexpected round %d", round)
			}
		}()
		return ch
	}

	collector := newTokenCollector(e)

	done := make(chan struct{})
	go NewProducer(e, source, agent).Run(context.Background(), []openai.ChatCompletionMessageParamUnion{openai.UserMessage("天气如何")}, done)
	waitDone(t, done, 2*time.Second)
	collector.wait(t, 2*time.Second)

	if e.LLMState() != LLMStateIdle {
		t.Errorf("llmState = %d after run, want %d", e.LLMState(), LLMStateIdle)
	}

	mu.Lock()
	if len(toolArgs) != 1 || toolArgs[0]["city"] != "北京" {
		t.Fatalf("tool args = %v, want one call with city=北京", toolArgs)
	}
	mu.Unlock()

	if len(round2Messages) != 3 {
		t.Fatalf("round 2 got %d messages, want 3 (user, assistant, tool)", len(round2Messages))
	}
	if round2Messages[1].OfAssistant == nil || len(round2Messages[1].OfAssistant.ToolCalls) != 1 {
		t.Fatalf("round 2 assistant message missing tool call: %+v", round2Messages[1])
	}
	if round2Messages[2].OfTool == nil || round2Messages[2].OfTool.Content.OfString.Value != "晴" {
		t.Fatalf("round 2 tool message wrong: %+v", round2Messages[2])
	}

	records := e.ToolInfo().Get()
	if len(records) != 1 || records[0].Name != "get_weather" || records[0].Result != "晴" {
		t.Fatalf("ToolInfo = %+v", records)
	}

	tokens := collector.all()
	assertStamped(t, tokens, gen)
	var text string
	var states []TokenState
	for _, tok := range tokens {
		text += tok.Content
		states = append(states, tok.State)
	}
	if text != "好的，北京今天晴。" {
		t.Errorf("reassembled text = %q", text)
	}
	if states[len(states)-1] != StateIdle {
		t.Errorf("last state = %q, want %q", states[len(states)-1], StateIdle)
	}
}

func TestProducerInterruptibleDuringContent(t *testing.T) {
	e := NewEngine(context.Background())

	contentSent := make(chan struct{})
	source := &fakeStreamSource{}
	source.fn = func(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) <-chan llm.Event {
		ch := make(chan llm.Event, 4)
		go func() {
			defer close(ch)
			ch <- llm.Event{Kind: llm.EventContent, Text: "你好"}
			close(contentSent)
			<-ctx.Done()
		}()
		return ch
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go NewProducer(e, source, agent_runtime.NewAgent(&agent_runtime.LLMConfig{}, nil)).Run(ctx, []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}, done)

	<-contentSent
	time.Sleep(50 * time.Millisecond) // let the producer block mid-stream
	cancel()

	waitDone(t, done, time.Second)

	if e.LLMState() != LLMStateIdle {
		t.Errorf("llmState = %d, want %d", e.LLMState(), LLMStateIdle)
	}
}

func TestProducerToolPhaseImmuneToCancel(t *testing.T) {
	e := NewEngine(context.Background())

	toolRan := make(chan struct{}, 1)
	var toolCtxErr error
	agent := agent_runtime.NewAgent(&agent_runtime.LLMConfig{}, []*agent_runtime.Tool{
		{
			Name: "remember",
			Func: func(ctx context.Context, args map[string]any) (string, error) {
				toolCtxErr = ctx.Err()
				toolRan <- struct{}{}
				return "ok", nil
			},
			Parameters: map[string]any{"type": "object"},
		},
	})

	release := make(chan struct{})
	source := &fakeStreamSource{}
	source.fn = func(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) <-chan llm.Event {
		ch := make(chan llm.Event, 8)
		go func() {
			defer close(ch)
			hasToolResult := false
			for _, m := range messages {
				if m.OfTool != nil {
					hasToolResult = true
				}
			}
			if !hasToolResult {
				// Round 1: latch the tool phase, then block regardless of ctx.
				ch <- llm.Event{Kind: llm.EventToolCall, ToolCall: llm.ToolCallPart{Index: 0, ID: "call_1", Name: "remember", Arguments: `{"k":"v"}`}}
				<-release
				ch <- llm.Event{Kind: llm.EventFinish, FinishReason: "tool_calls"}
			} else {
				ch <- llm.Event{Kind: llm.EventContent, Text: "记住了。"}
				ch <- llm.Event{Kind: llm.EventFinish, FinishReason: "stop"}
			}
		}()
		return ch
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go NewProducer(e, source, agent).Run(ctx, []openai.ChatCompletionMessageParamUnion{openai.UserMessage("记住这个")}, done)

	// The first tool delta latches llmState into the tool phase.
	waitFor(t, "tool phase latch", 2*time.Second, func() bool {
		return e.LLMState() == LLMStateTool
	})

	cancel()

	// Tool phase is immune: the producer must survive the cancellation.
	select {
	case <-done:
		t.Fatal("producer exited during tool phase despite cancel")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	select {
	case <-toolRan:
	case <-time.After(time.Second):
		t.Fatal("ExecuteToolCalls was not invoked")
	}
	if toolCtxErr != nil {
		t.Errorf("tool ran with cancelled ctx: %v", toolCtxErr)
	}

	waitDone(t, done, time.Second)

	if records := e.ToolInfo().Get(); len(records) != 1 || records[0].Name != "remember" {
		t.Fatalf("ToolInfo = %+v", records)
	}
}

func TestProducerWatchdogTimeout(t *testing.T) {
	e := NewEngine(context.Background())

	source := &fakeStreamSource{}
	source.fn = func(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) <-chan llm.Event {
		ch := make(chan llm.Event)
		go func() {
			defer close(ch)
			<-ctx.Done() // a stream that never produces, like a hung connection
		}()
		return ch
	}

	done := make(chan struct{})
	p := NewProducer(e, source, agent_runtime.NewAgent(&agent_runtime.LLMConfig{}, nil), WithWatchdog(50*time.Millisecond))
	go p.Run(context.Background(), []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}, done)

	waitDone(t, done, 2*time.Second)

	if e.LLMState() != LLMStateIdle {
		t.Errorf("llmState = %d, want %d", e.LLMState(), LLMStateIdle)
	}
}

func TestProducerErrorFinish(t *testing.T) {
	e := NewEngine(context.Background())

	source := &fakeStreamSource{}
	source.fn = func(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) <-chan llm.Event {
		ch := make(chan llm.Event, 2)
		go func() {
			defer close(ch)
			ch <- llm.Event{Kind: llm.EventFinish, FinishReason: "error", Text: "upstream boom"}
		}()
		return ch
	}

	errCh := make(chan error, 1)
	done := make(chan struct{})
	p := NewProducer(e, source, agent_runtime.NewAgent(&agent_runtime.LLMConfig{}, nil), WithOnError(func(err error) {
		errCh <- err
	}))
	go p.Run(context.Background(), []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}, done)

	select {
	case err := <-errCh:
		if err.Error() != "upstream boom" {
			t.Errorf("OnError got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("OnError was not called")
	}
	waitDone(t, done, time.Second)
}

func TestProducerResetsWatchdogOnActivity(t *testing.T) {
	e := NewEngine(context.Background())

	// Tokens arrive slower than the watchdog but keep the turn alive.
	source := &fakeStreamSource{}
	source.fn = func(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) <-chan llm.Event {
		ch := make(chan llm.Event, 8)
		go func() {
			defer close(ch)
			for _, r := range []rune("你好，世界。") {
				select {
				case ch <- llm.Event{Kind: llm.EventContent, Text: string(r)}:
				case <-ctx.Done():
					return
				}
				time.Sleep(40 * time.Millisecond)
			}
			ch <- llm.Event{Kind: llm.EventFinish, FinishReason: "stop"}
		}()
		return ch
	}

	collector := newTokenCollector(e)

	done := make(chan struct{})
	p := NewProducer(e, source, agent_runtime.NewAgent(&agent_runtime.LLMConfig{}, nil), WithWatchdog(200*time.Millisecond))
	start := time.Now()
	go p.Run(context.Background(), []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")}, done)
	waitDone(t, done, 5*time.Second)
	collector.wait(t, time.Second)

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("turn took %v, watchdog reset did not keep it alive", elapsed)
	}

	var text string
	for _, tok := range collector.all() {
		text += tok.Content
	}
	if text != "你好，世界。" {
		t.Errorf("reassembled text = %q", text)
	}
}

func TestProducerReportsTurnMessages(t *testing.T) {
	e := NewEngine(context.Background())

	var toolMu sync.Mutex
	var toolCalls []map[string]any
	agent := agent_runtime.NewAgent(&agent_runtime.LLMConfig{}, []*agent_runtime.Tool{
		{
			Name: "remember",
			Func: func(ctx context.Context, args map[string]any) (string, error) {
				toolMu.Lock()
				toolCalls = append(toolCalls, args)
				toolMu.Unlock()
				return "ok", nil
			},
			Parameters: map[string]any{"type": "object"},
		},
	})

	round := 0
	source := &fakeStreamSource{}
	source.fn = func(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, tools []openai.ChatCompletionToolUnionParam) <-chan llm.Event {
		round++
		ch := make(chan llm.Event, 8)
		go func() {
			defer close(ch)
			switch round {
			case 1:
				ch <- llm.Event{Kind: llm.EventToolCall, ToolCall: llm.ToolCallPart{Index: 0, ID: "call_1", Name: "remember", Arguments: `{"k":"v"}`}}
				ch <- llm.Event{Kind: llm.EventFinish, FinishReason: "tool_calls"}
			case 2:
				ch <- llm.Event{Kind: llm.EventContent, Text: "记住了。"}
				ch <- llm.Event{Kind: llm.EventFinish, FinishReason: "stop"}
			}
		}()
		return ch
	}

	producedCh := make(chan []openai.ChatCompletionMessageParamUnion, 1)
	p := NewProducer(e, source, agent, WithOnError(func(err error) { t.Errorf("OnError: %v", err) }))
	p.OnTurnMessages = func(msgs []openai.ChatCompletionMessageParamUnion) { producedCh <- msgs }

	done := make(chan struct{})
	input := []openai.ChatCompletionMessageParamUnion{openai.UserMessage("记住 k=v，然后告诉我。")}
	go p.Run(context.Background(), input, done)
	waitDone(t, done, 2*time.Second)

	select {
	case produced := <-producedCh:
		if len(produced) != 3 {
			t.Fatalf("produced = %d messages, want 3 (assistant+tool, assistant)", len(produced))
		}
		if produced[0].OfAssistant == nil || len(produced[0].OfAssistant.ToolCalls) != 1 {
			t.Fatalf("produced[0] not assistant tool_calls: %+v", produced[0])
		}
		if produced[1].OfTool == nil {
			t.Fatalf("produced[1] not tool message: %+v", produced[1])
		}
		if produced[2].OfAssistant == nil || !produced[2].OfAssistant.Content.OfString.Valid() || produced[2].OfAssistant.Content.OfString.Value != "记住了。" {
			t.Fatalf("produced[2] not final assistant text: %+v", produced[2])
		}
	case <-time.After(time.Second):
		t.Fatal("OnTurnMessages not called")
	}

	// The caller's slice must not have been mutated.
	if len(input) != 1 {
		t.Fatalf("input slice grew to %d, producer must work on a copy", len(input))
	}
	toolMu.Lock()
	defer toolMu.Unlock()
	if len(toolCalls) != 1 {
		t.Fatalf("tool called %d times, want 1", len(toolCalls))
	}
}
