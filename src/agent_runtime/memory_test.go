package agent_runtime

import (
	"testing"

	"github.com/openai/openai-go/v3"
)

func TestDefaultMemoryNoOp(t *testing.T) {
	a := &Agent{memory: &defaultMemory{}}
	msgs := []openai.ChatCompletionMessageParamUnion{
		systemMessage("sys"),
		userMessage("a"),
		userMessage("b"),
	}
	out := a.memory.Prepare(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
}

func TestSlideWindowKeepsSystem(t *testing.T) {
	m := &slideWindowMemory{maxMessages: 2}
	msgs := []openai.ChatCompletionMessageParamUnion{
		systemMessage("sys"),
		userMessage("1"),
		userMessage("2"),
		userMessage("3"),
	}
	out := m.Prepare(msgs)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	if out[0].OfSystem == nil {
		t.Fatalf("expected system message first")
	}
}

func TestCompressMemoryTruncates(t *testing.T) {
	m := &compressMemory{maxInputTokens: 10}
	msgs := []openai.ChatCompletionMessageParamUnion{
		systemMessage("sys"),
		userMessage("aaaaaaaaaa"),
		userMessage("bbbbbbbbbb"),
	}
	out := m.Prepare(msgs)
	if len(out) > 2 {
		t.Fatalf("expected truncation, got %d messages", len(out))
	}
	if out[0].OfSystem == nil {
		t.Fatalf("expected system message preserved")
	}
}

func systemMessage(content string) openai.ChatCompletionMessageParamUnion {
	return openai.ChatCompletionMessageParamUnion{
		OfSystem: &openai.ChatCompletionSystemMessageParam{
			Content: openai.ChatCompletionSystemMessageParamContentUnion{
				OfString: openai.String(content),
			},
		},
	}
}

func userMessage(content string) openai.ChatCompletionMessageParamUnion {
	return openai.UserMessage(content)
}
