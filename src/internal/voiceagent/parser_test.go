package voiceagent

import (
	"testing"
)

func TestStreamParserPlainText(t *testing.T) {
	p := NewStreamParser()
	spoken, calls := p.Feed("hello world")
	if len(calls) != 0 {
		t.Fatalf("expected no tool calls, got %v", calls)
	}
	if spoken != "hello world" {
		t.Fatalf("expected 'hello world', got %q", spoken)
	}
	if flushed := p.Flush(); flushed != "" {
		t.Fatalf("expected empty flush, got %q", flushed)
	}
}

func TestStreamParserSingleToolCall(t *testing.T) {
	p := NewStreamParser()
	spoken, calls := p.Feed(`ok <tool_call>{"name": "update_requirements", "arguments": {"topic": "math"}}</tool_call> done`)
	if spoken != "ok  done" {
		t.Fatalf("expected 'ok  done', got %q", spoken)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Name != "update_requirements" {
		t.Fatalf("expected update_requirements, got %q", calls[0].Name)
	}
	if calls[0].Arguments["topic"] != "math" {
		t.Fatalf("expected topic=math, got %v", calls[0].Arguments)
	}
}

func TestStreamParserSplitAcrossTokens(t *testing.T) {
	p := NewStreamParser()
	var spoken string
	s, _ := p.Feed(`say <tool_call>{"name": "`)
	spoken += s
	s, _ = p.Feed(`update_requirements", "arguments": {`)
	spoken += s
	s, calls := p.Feed(`"topic": "math"}}</tool_call> end`)
	spoken += s

	if spoken != "say  end" {
		t.Fatalf("expected 'say  end', got %q", spoken)
	}
	if len(calls) != 1 || calls[0].Name != "update_requirements" {
		t.Fatalf("unexpected tool calls: %v", calls)
	}
}

func TestStreamParserMultipleToolCalls(t *testing.T) {
	p := NewStreamParser()
	_, calls := p.Feed(`<tool_call>{"name": "a", "arguments": {}}</tool_call><tool_call>{"name": "b", "arguments": {}}</tool_call>`)
	if len(calls) != 2 || calls[0].Name != "a" || calls[1].Name != "b" {
		t.Fatalf("unexpected calls: %v", calls)
	}
}

func TestParseToolCallJSON(t *testing.T) {
	tc, err := ParseToolCallJSON(`{"name": "send_to_ppt_agent", "arguments": {"data": "feedback"}}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tc.Name != "send_to_ppt_agent" {
		t.Fatalf("expected send_to_ppt_agent, got %q", tc.Name)
	}
	if tc.Arguments["data"] != "feedback" {
		t.Fatalf("unexpected arguments: %v", tc.Arguments)
	}
}
