package voiceagent

import (
	"fmt"

	"educationagent/internal/toolcalling"
)

// ParseToolCall parses one compact tool_call payload emitted by the finetuned
// voice agent, e.g. "send_to_arm_agent:抓取 red 物块并放到 (1.0,2.0,3.0)。" or
// "get_message_from_arm_agent:". The voice agent's tools take at most one
// free-form argument, so everything after the first ":" is passed through
// verbatim (commas and colons inside the content survive).
func ParseToolCall(payload string) (name string, args map[string]string, err error) {
	name, raw := toolcalling.ParseCompactPayload(payload)
	if name == "" {
		return "", nil, fmt.Errorf("empty tool_call payload")
	}
	args = make(map[string]string)
	if raw != "" {
		args["content"] = raw
	}
	return name, args, nil
}
