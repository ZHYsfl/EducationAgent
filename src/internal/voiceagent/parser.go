package voiceagent

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	toolCallStart = "<tool_call>"
	toolCallEnd   = "</tool_call>"
)

// ToolCall represents a parsed Qwen3 tool call.
type ToolCall struct {
	Name      string
	Arguments map[string]any
}

// StreamParser extracts complete <tool_call> blocks from a streaming LLM
// response while passing through ordinary spoken text.
type StreamParser struct {
	buf strings.Builder
}

// NewStreamParser creates a new stream parser.
func NewStreamParser() *StreamParser {
	return &StreamParser{}
}

// Feed consumes a new token. It returns any newly completed spoken text and any
// newly completed tool calls. Incomplete tool-call content is buffered for the
// next call.
func (p *StreamParser) Feed(token string) (spoken string, calls []ToolCall) {
	p.buf.WriteString(token)
	s := p.buf.String()

	for {
		startIdx := strings.Index(s, toolCallStart)
		if startIdx == -1 {
			break
		}
		// Flush any spoken text before the tool call.
		if startIdx > 0 {
			spoken += s[:startIdx]
		}
		endIdx := strings.Index(s[startIdx:], toolCallEnd)
		if endIdx == -1 {
			// Tool call not yet complete; keep buffered from startIdx.
			p.buf.Reset()
			p.buf.WriteString(s[startIdx:])
			return spoken, calls
		}
		payloadStart := startIdx + len(toolCallStart)
		payloadEnd := startIdx + endIdx
		payload := strings.TrimSpace(s[payloadStart:payloadEnd])

		if tc, err := ParseToolCallJSON(payload); err == nil {
			calls = append(calls, tc)
		}
		s = s[startIdx+endIdx+len(toolCallEnd):]
	}

	// No open tool call; if the remaining text does not contain the start of a
	// future tool call, emit it as spoken text now.
	p.buf.Reset()
	if strings.Index(s, toolCallStart) == -1 {
		spoken += s
		return spoken, calls
	}
	if len(s) > 0 {
		p.buf.WriteString(s)
	}
	return spoken, calls
}

// Flush returns any remaining buffered text as spoken content.
func (p *StreamParser) Flush() string {
	s := p.buf.String()
	p.buf.Reset()
	return s
}

// ParseToolCallJSON parses the JSON payload inside a <tool_call> tag.
func ParseToolCallJSON(payload string) (ToolCall, error) {
	var raw struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return ToolCall{}, fmt.Errorf("parse tool call json: %w", err)
	}
	if raw.Name == "" {
		return ToolCall{}, fmt.Errorf("tool call missing name")
	}
	if raw.Arguments == nil {
		raw.Arguments = make(map[string]any)
	}
	return ToolCall{Name: raw.Name, Arguments: raw.Arguments}, nil
}
