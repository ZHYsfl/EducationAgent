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

func TestPptHistoryAlignToolCallStart_IncludesAssistantAndTools(t *testing.T) {
	asst := openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			ToolCalls: []openai.ChatCompletionMessageToolCallUnionParam{
				{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: "call_1",
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      "read_file",
							Arguments: "{}",
						},
					},
				},
			},
		},
	}
	tool := openai.ToolMessage(`{"ok":true}`, "call_1")
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("sys"),
		openai.UserMessage("u1"),
		asst,
		tool,
		openai.UserMessage("u2"),
	}
	// Tail slice starting at lone tool message is invalid for APIs.
	assert.False(t, pptHistoryToolSuffixValid(msgs, 3))
	start := pptHistoryAlignToolCallStart(msgs, 3)
	assert.Equal(t, 2, start)
	assert.True(t, pptHistoryToolSuffixValid(msgs, start))
}
