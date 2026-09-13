package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func collectEvents(ch <-chan Event) []Event {
	var events []Event
	for ev := range ch {
		events = append(events, ev)
	}
	return events
}

func TestStreamContent(t *testing.T) {
	ch := eventsFromStream(context.Background(), streamFromReader(strings.NewReader(readFixture(t, "content_stream.sse"))))

	events := collectEvents(ch)

	want := []Event{
		{Kind: EventContent, Text: "杭州"},
		{Kind: EventContent, Text: "，"},
		{Kind: EventContent, Text: "是"},
		{Kind: EventContent, Text: "浙江"},
		{Kind: EventFinish, FinishReason: "stop"},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(want), events)
	}
	for i, w := range want {
		if events[i] != w {
			t.Errorf("event[%d] = %+v, want %+v", i, events[i], w)
		}
	}
}

func TestStreamToolCall(t *testing.T) {
	ch := eventsFromStream(context.Background(), streamFromReader(strings.NewReader(readFixture(t, "tool_call_stream.sse"))))

	events := collectEvents(ch)

	if len(events) != 6 {
		t.Fatalf("got %d events, want 6: %+v", len(events), events)
	}
	for i, ev := range events[:5] {
		if ev.Kind != EventToolCall {
			t.Fatalf("event[%d] kind = %v, want EventToolCall", i, ev.Kind)
		}
		if ev.ToolCall.Index != 0 {
			t.Errorf("event[%d] index = %d, want 0", i, ev.ToolCall.Index)
		}
	}
	first := events[0].ToolCall
	if first.ID != "call_00_abc" || first.Name != "get_weather" || first.Arguments != "" {
		t.Errorf("first fragment = %+v", first)
	}
	gotArgs := ""
	for _, ev := range events[:5] {
		gotArgs += ev.ToolCall.Arguments
	}
	if wantArgs := `{"city": "北京"}`; gotArgs != wantArgs {
		t.Errorf("arguments = %q, want %q", gotArgs, wantArgs)
	}
	if last := events[5]; last.Kind != EventFinish || last.FinishReason != "tool_calls" {
		t.Errorf("last event = %+v, want Finish/tool_calls", last)
	}
}

func TestStreamContextCancelClosesChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := eventsFromStream(ctx, streamFromReader(strings.NewReader(readFixture(t, "content_stream.sse"))))

	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("channel did not close after ctx cancel")
		}
	}
}

func TestStreamErrorBecomesFinishEvent(t *testing.T) {
	// The second frame is not valid JSON: the decode fails mid-stream, after
	// the channel already exists, so the error must surface as an event.
	bad := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"杭\"},\"finish_reason\":null}]}\n\n" +
		"data: {broken}\n\n" +
		"data: [DONE]\n\n"
	ch := eventsFromStream(context.Background(), streamFromReader(strings.NewReader(bad)))

	events := collectEvents(ch)

	if len(events) == 0 || events[0].Kind != EventContent || events[0].Text != "杭" {
		t.Fatalf("events = %+v, want leading content event", events)
	}
	last := events[len(events)-1]
	if last.Kind != EventFinish || last.FinishReason != "error" || last.Text == "" {
		t.Fatalf("last event = %+v, want Finish/error with message", last)
	}
}
