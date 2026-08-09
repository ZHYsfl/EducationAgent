package service

import (
	"fmt"
	"testing"

	"educationagent/internal/model"
	"github.com/stretchr/testify/assert"
)

func collectChunks(extractor *streamExtractor, out chan model.SSEChunk) []model.SSEChunk {
	close(out)
	var chunks []model.SSEChunk
	for c := range out {
		chunks = append(chunks, c)
	}
	return chunks
}

func TestStreamExtractorPlainText(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, nil)

	extractor.Feed("hello world")
	extractor.Flush()

	chunks := collectChunks(extractor, out)
	var text string
	for _, c := range chunks {
		assert.Equal(t, "tts", c.Type)
		text += c.Text
	}
	assert.Equal(t, "hello world", text)
}

func TestStreamExtractorSingleToolCall(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string { return "发送成功" })

	extractor.Feed("好的，")
	extractor.Feed("<tool_call>")
	extractor.Feed("\nsend_to_arm_agent:抓取 red 物块\n")
	extractor.Feed("</tool_call>")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 3)
	require.Equal("tts", chunks[0].Type)
	require.Equal("好的，", chunks[0].Text)
	require.Equal("action", chunks[1].Type)
	require.Equal("\nsend_to_arm_agent:抓取 red 物块\n", chunks[1].Payload)
	require.Equal("tool", chunks[2].Type)
	require.Equal("发送成功", chunks[2].Text)
	require.Equal([]string{"发送成功"}, extractor.toolResults)
}

func TestStreamExtractorSplitToolCallTag(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string { return "done" })

	// <tool_call> split across tokens
	extractor.Feed("hello <tool_")
	extractor.Feed("call>data</tool_call>")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 3)
	require.Equal("tts", chunks[0].Type)
	require.Equal("hello ", chunks[0].Text)
	require.Equal("action", chunks[1].Type)
	require.Equal("data", chunks[1].Payload)
	require.Equal("tool", chunks[2].Type)
	require.Equal("done", chunks[2].Text)
	require.Equal([]string{"done"}, extractor.toolResults)
}

func TestStreamExtractorUnclosedToolCall(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, nil)

	extractor.Feed("text <tool_call>unclosed")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 2)
	require.Equal("tts", chunks[0].Type)
	require.Equal("text ", chunks[0].Text)
	// Last chunk should contain the unclosed tool_call as plain text.
	last := chunks[len(chunks)-1]
	require.Equal("tts", last.Type)
	require.Equal("<tool_call>unclosed", last.Text)
}

func TestStreamExtractorMultipleToolCalls(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	counter := 0
	extractor := newStreamExtractor(out, func(p string) string {
		counter++
		return fmt.Sprintf("result%d", counter)
	})

	extractor.Feed("<tool_call>a1</tool_call> mid <tool_call>a2</tool_call>")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 5)
	require.Equal("action", chunks[0].Type)
	require.Equal("a1", chunks[0].Payload)
	require.Equal("tool", chunks[1].Type)
	require.Equal("result1", chunks[1].Text)
	require.Equal("tts", chunks[2].Type)
	require.Equal(" mid ", chunks[2].Text)
	require.Equal("action", chunks[3].Type)
	require.Equal("a2", chunks[3].Payload)
	require.Equal("tool", chunks[4].Type)
	require.Equal("result2", chunks[4].Text)
	require.Equal([]string{"result1", "result2"}, extractor.toolResults)
}

func TestStreamExtractorToolCallCallback(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	var payloads []string
	extractor := newStreamExtractor(out, func(p string) string {
		payloads = append(payloads, p)
		return "ok"
	})

	extractor.Feed("<tool_call>send_to_arm_agent:抓取 red 物块</tool_call>")
	extractor.Feed("<tool_call>get_message_from_arm_agent:</tool_call>")
	extractor.Flush()

	_ = collectChunks(extractor, out)

	assert.Equal(t, []string{"send_to_arm_agent:抓取 red 物块", "get_message_from_arm_agent:"}, payloads)
}

func TestStreamExtractorHistoryPlainText(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, nil)

	extractor.Feed("hello world")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "hello world", extractor.history.String())
}

func TestStreamExtractorHistorySingleToolCall(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string { return "发送成功" })

	extractor.Feed("好的，")
	extractor.Feed("<tool_call>")
	extractor.Feed("\nsend_to_arm_agent:抓取 red 物块\n")
	extractor.Feed("</tool_call>")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "好的，<tool_call>\nsend_to_arm_agent:抓取 red 物块\n</tool_call>", extractor.history.String())
	assert.Equal(t, []string{"发送成功"}, extractor.toolResults)
}

func TestStreamExtractorHistoryMultipleToolCalls(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string {
		if p == "a1" {
			return "result1"
		}
		return "result2"
	})

	extractor.Feed("<tool_call>a1</tool_call> mid <tool_call>a2</tool_call>")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "<tool_call>a1</tool_call> mid <tool_call>a2</tool_call>", extractor.history.String())
	assert.Equal(t, []string{"result1", "result2"}, extractor.toolResults)
}

func TestStreamExtractorHistorySplitToolCallTag(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string { return "done" })

	extractor.Feed("hello <tool_")
	extractor.Feed("call>data</tool_call>")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "hello <tool_call>data</tool_call>", extractor.history.String())
	assert.Equal(t, []string{"done"}, extractor.toolResults)
}

func TestStreamExtractorHistoryUnclosedToolCall(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, nil)

	extractor.Feed("text <tool_call>unclosed")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "text <tool_call>unclosed", extractor.history.String())
}

func TestStreamExtractorActions(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(string) string { return "ok" })
	extractor.Feed("好的，")
	extractor.Feed("<tool_call>send_to_arm_agent:抓取 red 物块</tool_call>")
	extractor.Feed("<tool_call>get_message_from_arm_agent:</tool_call>")
	extractor.Flush()
	_ = collectChunks(extractor, out)

	assert.Equal(t, []string{"send_to_arm_agent:抓取 red 物块", "get_message_from_arm_agent:"}, extractor.actions)
}
