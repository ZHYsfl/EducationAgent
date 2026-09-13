package agent_runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/openai/openai-go/v3"
)

func toolMsgContent(t *testing.T, msg openai.ChatCompletionMessageParamUnion) string {
	t.Helper()
	if msg.OfTool == nil {
		t.Fatalf("expected tool message, got %+v", msg)
	}
	if !msg.OfTool.Content.OfString.Valid() {
		t.Fatalf("expected string tool content")
	}
	return msg.OfTool.Content.OfString.Value
}

func TestExecuteToolCallsSuccess(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, []*Tool{
		{
			Name:        "get_weather",
			Description: "Get weather",
			Func: func(ctx context.Context, args map[string]any) (string, error) {
				return "sunny in " + args["city"].(string), nil
			},
			Parameters: map[string]any{
				"type":     "object",
				"required": []any{"city"},
			},
		},
	})

	calls := []ToolCallInput{
		{ID: "call_1", Name: "get_weather", ArgumentsJSON: `{"city":"Beijing"}`},
	}
	msgs := agent.ExecuteToolCalls(context.Background(), calls)

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := toolMsgContent(t, msgs[0]); got != "sunny in Beijing" {
		t.Errorf("unexpected tool result: %q", got)
	}
}

func TestExecuteToolCallsPreservesCallOrder(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, []*Tool{
		{
			Name: "echo",
			Func: func(ctx context.Context, args map[string]any) (string, error) {
				return args["word"].(string), nil
			},
			Parameters: map[string]any{"type": "object"},
		},
	})

	calls := []ToolCallInput{
		{ID: "call_1", Name: "echo", ArgumentsJSON: `{"word":"first"}`},
		{ID: "call_2", Name: "echo", ArgumentsJSON: `{"word":"second"}`},
		{ID: "call_3", Name: "echo", ArgumentsJSON: `{"word":"third"}`},
	}
	msgs := agent.ExecuteToolCalls(context.Background(), calls)

	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	for i, want := range []string{"first", "second", "third"} {
		if got := toolMsgContent(t, msgs[i]); got != want {
			t.Errorf("msgs[%d] = %q, want %q", i, got, want)
		}
	}
}

func TestExecuteToolCallsParseError(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, []*Tool{
		{Name: "noop", Func: func(ctx context.Context, args map[string]any) (string, error) { return "ok", nil }},
	})

	msgs := agent.ExecuteToolCalls(context.Background(), []ToolCallInput{
		{ID: "call_1", Name: "noop", ArgumentsJSON: `{not json`},
	})

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := toolMsgContent(t, msgs[0]); !strings.Contains(got, "[PARSE_ERROR]") {
		t.Errorf("expected PARSE_ERROR, got %q", got)
	}
}

func TestExecuteToolCallsArgError(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, []*Tool{
		{
			Name: "need_arg",
			Func: func(ctx context.Context, args map[string]any) (string, error) { return "ok", nil },
			Parameters: map[string]any{
				"required": []any{"city"},
			},
		},
	})

	msgs := agent.ExecuteToolCalls(context.Background(), []ToolCallInput{
		{ID: "call_1", Name: "need_arg", ArgumentsJSON: `{"other":1}`},
	})

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := toolMsgContent(t, msgs[0]); !strings.Contains(got, "[ARG_ERROR]") {
		t.Errorf("expected ARG_ERROR, got %q", got)
	}
}

func TestExecuteToolCallsNotFound(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, nil)

	msgs := agent.ExecuteToolCalls(context.Background(), []ToolCallInput{
		{ID: "call_1", Name: "ghost", ArgumentsJSON: `{}`},
	})

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := toolMsgContent(t, msgs[0]); !strings.Contains(got, "[NOT_FOUND]") {
		t.Errorf("expected NOT_FOUND, got %q", got)
	}
}

func TestExecuteToolCallsExecError(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, []*Tool{
		{
			Name: "boom",
			Func: func(ctx context.Context, args map[string]any) (string, error) {
				return "", errors.New("kaboom")
			},
		},
	})

	msgs := agent.ExecuteToolCalls(context.Background(), []ToolCallInput{
		{ID: "call_1", Name: "boom", ArgumentsJSON: `{}`},
	})

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if got := toolMsgContent(t, msgs[0]); !strings.Contains(got, "[EXEC_ERROR]") {
		t.Errorf("expected EXEC_ERROR, got %q", got)
	}
}

func TestExecuteToolCallsEmpty(t *testing.T) {
	agent := NewAgent(&LLMConfig{}, nil)
	msgs := agent.ExecuteToolCalls(context.Background(), nil)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestExecuteToolCallsConcurrent(t *testing.T) {
	var mu sync.Mutex
	active := 0
	maxActive := 0
	agent := NewAgent(&LLMConfig{}, []*Tool{
		{
			Name: "probe",
			Func: func(ctx context.Context, args map[string]any) (string, error) {
				mu.Lock()
				active++
				if active > maxActive {
					maxActive = active
				}
				mu.Unlock()
				return "ok", nil
			},
		},
	})

	calls := make([]ToolCallInput, 8)
	for i := range calls {
		calls[i] = ToolCallInput{ID: "call", Name: "probe", ArgumentsJSON: `{}`}
	}
	msgs := agent.ExecuteToolCalls(context.Background(), calls)

	if len(msgs) != len(calls) {
		t.Fatalf("expected %d messages, got %d", len(calls), len(msgs))
	}
	if maxActive < 2 {
		t.Errorf("expected concurrent execution, max active = %d", maxActive)
	}
}
