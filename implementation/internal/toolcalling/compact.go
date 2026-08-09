package toolcalling

import (
	"strings"
)

// Compact tool_call format (finetuning contract, see finetuning_of_arm_agent.md
// §2 / finetuning_of_voice_agent.md §2): the finetuned models emit tool calls
// inline as text, NOT as OpenAI JSON function calls:
//
//	<tool_call>
//	function_name:arg1,arg2
//	</tool_call>
//
// The payload after the first ":" is tool-specific: multi-parameter tools
// (move_to_coordinates) split it by ",", single-parameter tools
// (send_to_voice_agent, grab_the_block, ...) take it verbatim so commas
// inside free-form content survive.

const (
	ToolCallOpenTag  = "<tool_call>"
	ToolCallCloseTag = "</tool_call>"
)

// CompactToolCall is one parsed <tool_call> block.
type CompactToolCall struct {
	Name    string
	RawArgs string // everything after the first ":", untrimmed of inner content
}

// ParseCompactToolCalls extracts every <tool_call>...</tool_call> block from a
// complete assistant message.
func ParseCompactToolCalls(content string) []CompactToolCall {
	var calls []CompactToolCall
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
		name, raw := ParseCompactPayload(payload)
		if name == "" {
			continue
		}
		calls = append(calls, CompactToolCall{Name: name, RawArgs: raw})
	}
	return calls
}

// ParseCompactPayload splits one tool_call payload ("name:arg1,arg2") into the
// tool name and the raw argument string. A payload without ":" (or with an
// empty argument part) yields empty RawArgs.
func ParseCompactPayload(payload string) (name string, rawArgs string) {
	payload = strings.TrimSpace(payload)
	if i := strings.Index(payload, ":"); i >= 0 {
		return strings.TrimSpace(payload[:i]), strings.TrimSpace(payload[i+1:])
	}
	return payload, ""
}

// SplitCompactArgs splits RawArgs positionally by "," for multi-parameter
// tools (e.g. move_to_coordinates:x,y,z). Each part is space-trimmed.
func SplitCompactArgs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
