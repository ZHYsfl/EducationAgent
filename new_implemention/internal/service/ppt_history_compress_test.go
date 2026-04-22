package service

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
)

func TestEstimatePPTTextTokens(t *testing.T) {
	assert.Equal(t, 0, estimatePPTTextTokens(""))
	assert.Equal(t, 1, estimatePPTTextTokens("a"))
	assert.Equal(t, 4, estimatePPTTextTokens("0123456789")) // (10+2)/3
	assert.Equal(t, 10, estimatePPTTextTokens(string(make([]byte, 30))))
}

func TestEstimatePPTHistoryTokens_UserMessage(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("sys"),
		openai.UserMessage("hello world"),
	}
	n := estimatePPTHistoryTokens(msgs)
	assert.Greater(t, n, 0)
}
