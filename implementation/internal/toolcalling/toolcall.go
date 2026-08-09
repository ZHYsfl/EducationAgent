package toolcalling

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Qwen3 native tool_call format (finetuning contract, see
// finetuning_of_arm_agent.md §2 / finetuning_of_voice_agent.md §2): the
// finetuned models emit tool calls inline as text — exactly how the Qwen3
// chat template renders structured tool_calls:
//
//	<tool_call>
//	{"name": "function_name", "arguments": {"arg": "value"}}
//	</tool_call>
//
// The payload between the tags is one JSON object with "name" and
// "arguments" (a JSON object). Free-form string arguments (e.g. the content
// of send_to_voice_agent) survive commas and colons untouched because they
// are JSON string values, not positional segments.

const (
	ToolCallOpenTag  = "<tool_call>"
	ToolCallCloseTag = "</tool_call>"
)

// ToolCallBlock is one parsed <tool_call> block.
type ToolCallBlock struct {
	Name      string
	Arguments map[string]any
	Raw       string // raw JSON payload between the tags
}

// ParseToolCallBlocks extracts every <tool_call>...</tool_call> block from a
// complete assistant message. Blocks whose payload is not valid tool_call
// JSON are skipped.
func ParseToolCallBlocks(content string) []ToolCallBlock {
	var calls []ToolCallBlock
	rest := content
	for {
		start := strings.Index(rest, ToolCallOpenTag)
		if start < 0 {
			break
		}
		rest = rest[start+len(ToolCallOpenTag):]
		end := strings.Index(rest, ToolCallCloseTag)
		if end < 0 {
			break
		}
		payload := rest[:end]
		rest = rest[end+len(ToolCallCloseTag):]
		call, err := ParseToolCallPayload(payload)
		if err != nil {
			continue
		}
		calls = append(calls, call)
	}
	return calls
}

// ParseToolCallPayload parses one tool_call payload:
// {"name": "function_name", "arguments": {...}}. A missing "arguments"
// object yields an empty map.
func ParseToolCallPayload(payload string) (ToolCallBlock, error) {
	raw := strings.TrimSpace(payload)
	var wire struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return ToolCallBlock{}, fmt.Errorf("parse tool_call payload: %w", err)
	}
	if wire.Name == "" {
		return ToolCallBlock{}, fmt.Errorf("tool_call payload missing name")
	}
	if wire.Arguments == nil {
		wire.Arguments = map[string]any{}
	}
	return ToolCallBlock{Name: wire.Name, Arguments: wire.Arguments, Raw: raw}, nil
}

// StringArg returns an argument coerced to string (the embodied and
// communication tools take string-typed parameters).
func StringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
