package voiceagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseActionSimple(t *testing.T) {
	name, args, err := ParseAction("update_requirements|topic:math|style:simple")
	require.NoError(t, err)
	assert.Equal(t, "update_requirements", name)
	assert.Equal(t, map[string]string{"topic": "math", "style": "simple"}, args)
}

func TestParseActionNoArgs(t *testing.T) {
	name, args, err := ParseAction("require_confirm")
	require.NoError(t, err)
	assert.Equal(t, "require_confirm", name)
	assert.Empty(t, args)
}

func TestParseActionEmptyValue(t *testing.T) {
	name, args, err := ParseAction("send_to_ppt_agent|data:")
	require.NoError(t, err)
	assert.Equal(t, "send_to_ppt_agent", name)
	assert.Equal(t, map[string]string{"data": ""}, args)
}

func TestParseActionEmptyPayload(t *testing.T) {
	_, _, err := ParseAction("")
	assert.Error(t, err)
}

func TestArgsToMap(t *testing.T) {
	args := map[string]string{"topic": "math", "total_pages": "15", "style": "simple"}
	m := ArgsToMap(args, "total_pages")
	assert.Equal(t, "math", m["topic"])
	assert.Equal(t, 15, m["total_pages"])
	assert.Equal(t, "simple", m["style"])
}

func TestArgsToMapNonNumericIntField(t *testing.T) {
	args := map[string]string{"total_pages": "abc"}
	m := ArgsToMap(args, "total_pages")
	assert.Equal(t, "abc", m["total_pages"])
}
