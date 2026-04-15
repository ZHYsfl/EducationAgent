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
	e.Register("greet", func(ctx context.Context, args map[string]string) (string, error) {
		return "hello " + args["name"], nil
	})

	res, err := e.Execute(context.Background(), "greet|name:world")
	require.NoError(t, err)
	assert.Equal(t, "hello world", res)
}

func TestExecutorUnknownAction(t *testing.T) {
	e := NewExecutor()
	_, err := e.Execute(context.Background(), "unknown|x:1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

func TestExecutorActionError(t *testing.T) {
	e := NewExecutor()
	e.Register("fail", func(ctx context.Context, args map[string]string) (string, error) {
		return "", errors.New("boom")
	})

	_, err := e.Execute(context.Background(), "fail")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
