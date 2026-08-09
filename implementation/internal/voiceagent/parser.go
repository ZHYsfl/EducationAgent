package voiceagent

import (
	"educationagent/internal/toolcalling"
)

// ParseToolCall parses one Qwen3 native tool_call payload emitted by the
// finetuned voice agent, e.g.
// {"name": "send_to_arm_agent", "arguments": {"content": "抓取 red 物块并放到 (1.0,2.0,3.0)。"}}
// or {"name": "get_message_from_arm_agent", "arguments": {}}. All argument
// values are coerced to strings (the voice agent's tools take string-typed
// parameters).
func ParseToolCall(payload string) (name string, args map[string]string, err error) {
	call, err := toolcalling.ParseToolCallPayload(payload)
	if err != nil {
		return "", nil, err
	}
	args = make(map[string]string, len(call.Arguments))
	for k := range call.Arguments {
		args[k] = toolcalling.StringArg(call.Arguments, k)
	}
	return call.Name, args, nil
}
