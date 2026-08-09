package voiceagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseToolCallWithContent(t *testing.T) {
	name, args, err := ParseToolCall(`{"name": "send_to_arm_agent", "arguments": {"content": "抓取 red 物块并放到 (1.0,2.0,3.0)。"}}`)
	require.NoError(t, err)
	assert.Equal(t, "send_to_arm_agent", name)
	assert.Equal(t, map[string]string{"content": "抓取 red 物块并放到 (1.0,2.0,3.0)。"}, args)
}

func TestParseToolCallNoArgs(t *testing.T) {
	name, args, err := ParseToolCall(`{"name": "get_message_from_arm_agent", "arguments": {}}`)
	require.NoError(t, err)
	assert.Equal(t, "get_message_from_arm_agent", name)
	assert.Empty(t, args)
}

func TestParseToolCallMissingArguments(t *testing.T) {
	name, args, err := ParseToolCall(`{"name": "get_message_from_arm_agent"}`)
	require.NoError(t, err)
	assert.Equal(t, "get_message_from_arm_agent", name)
	assert.Empty(t, args)
}

func TestParseToolCallContentKeepsCommasAndColons(t *testing.T) {
	// Free-form content must pass through verbatim (JSON string value).
	name, args, err := ParseToolCall(`{"name": "send_to_arm_agent", "arguments": {"content": "先移动到 (0.5,0.2,0.1)，注意: 别碰红块"}}`)
	require.NoError(t, err)
	assert.Equal(t, "send_to_arm_agent", name)
	assert.Equal(t, "先移动到 (0.5,0.2,0.1)，注意: 别碰红块", args["content"])
}

func TestParseToolCallEmptyPayload(t *testing.T) {
	_, _, err := ParseToolCall("")
	assert.Error(t, err)
}

func TestParseToolCallMalformedJSON(t *testing.T) {
	_, _, err := ParseToolCall("send_to_arm_agent:抓取 red 物块")
	assert.Error(t, err)
}
