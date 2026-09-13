package llm

import "testing"

func TestToolCallAccumulator(t *testing.T) {
	var acc ToolCallAccumulator
	parts := []ToolCallPart{
		{Index: 0, ID: "call_00_abc", Name: "get_weather", Arguments: ""},
		{Index: 0, Arguments: "{"},
		{Index: 0, Arguments: `"city"`},
		{Index: 0, Arguments: `: "北京"`},
		{Index: 0, Arguments: "}"},
	}
	for _, p := range parts {
		acc.Add(p)
	}

	calls := acc.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1: %+v", len(calls), calls)
	}
	want := AccumulatedToolCall{
		ID:            "call_00_abc",
		Name:          "get_weather",
		ArgumentsJSON: `{"city": "北京"}`,
	}
	if calls[0] != want {
		t.Errorf("call = %+v, want %+v", calls[0], want)
	}
}

func TestToolCallAccumulatorMultipleSlots(t *testing.T) {
	var acc ToolCallAccumulator
	acc.Add(ToolCallPart{Index: 1, ID: "call_01", Name: "get_time", Arguments: `{"tz":`})
	acc.Add(ToolCallPart{Index: 0, ID: "call_00", Name: "get_weather", Arguments: `{"city":`})
	acc.Add(ToolCallPart{Index: 1, Arguments: ` "UTC"}`})

	calls := acc.ToolCalls()
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2: %+v", len(calls), calls)
	}
	if calls[0].ID != "call_00" || calls[0].ArgumentsJSON != `{"city":` {
		t.Errorf("call[0] = %+v", calls[0])
	}
	if calls[1].ID != "call_01" || calls[1].Name != "get_time" || calls[1].ArgumentsJSON != `{"tz": "UTC"}` {
		t.Errorf("call[1] = %+v", calls[1])
	}
}
