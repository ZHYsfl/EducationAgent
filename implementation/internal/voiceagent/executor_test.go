package voiceagent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutorSuccess(t *testing.T) {
	e := NewExecutor()
	e.Register("send_to_arm_agent", func(ctx context.Context, args map[string]string) (string, error) {
		return "发送成功:" + args["content"], nil
	})

	res, err := e.Execute(context.Background(), `{"name": "send_to_arm_agent", "arguments": {"content": "抓取 red 物块"}}`)
	require.NoError(t, err)
	assert.Equal(t, "发送成功:抓取 red 物块", res)
}

func TestExecutorUnknownTool(t *testing.T) {
	e := NewExecutor()
	_, err := e.Execute(context.Background(), `{"name": "unknown_tool", "arguments": {"content": "x"}}`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestExecutorToolError(t *testing.T) {
	e := NewExecutor()
	e.Register("fail", func(ctx context.Context, args map[string]string) (string, error) {
		return "", errors.New("boom")
	})

	_, err := e.Execute(context.Background(), `{"name": "fail", "arguments": {}}`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
