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

func TestStreamExtractorSingleAction(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string { return "ok" })

	extractor.Feed("ok ")
	extractor.Feed("<action>")
	extractor.Feed("update_requirements|topic:math")
	extractor.Feed("</action>")
	extractor.Feed(" done")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 4)
	require.Equal("tts", chunks[0].Type)
	require.Equal("ok ", chunks[0].Text)
	require.Equal("action", chunks[1].Type)
	require.Equal("update_requirements|topic:math", chunks[1].Payload)
	require.Equal("tool", chunks[2].Type)
	require.Equal("ok", chunks[2].Text)
	require.Equal("tts", chunks[3].Type)
	require.Equal(" done", chunks[3].Text)
	require.Equal([]string{"ok"}, extractor.toolResults)
	require.Equal([]string{"voice-agent-action-0"}, extractor.toolCallIDs)
}

func TestStreamExtractorSplitActionTag(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string { return "done" })

	// <action> split across tokens
	extractor.Feed("hello <act")
	extractor.Feed("ion>data</action>")
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
	require.Equal([]string{"voice-agent-action-0"}, extractor.toolCallIDs)
}

func TestStreamExtractorUnclosedAction(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, nil)

	extractor.Feed("text <action>unclosed")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 2)
	require.Equal("tts", chunks[0].Type)
	require.Equal("text ", chunks[0].Text)
	// Last chunk should contain the unclosed action as plain text.
	last := chunks[len(chunks)-1]
	require.Equal("tts", last.Type)
	require.Equal("<action>unclosed", last.Text)
}

func TestStreamExtractorMultipleActions(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	counter := 0
	extractor := newStreamExtractor(out, func(p string) string {
		counter++
		return fmt.Sprintf("result%d", counter)
	})

	extractor.Feed("<action>a1</action> mid <action>a2</action>")
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
	require.Equal([]string{"voice-agent-action-0", "voice-agent-action-1"}, extractor.toolCallIDs)
}

func TestStreamExtractorActionCallback(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	var payloads []string
	extractor := newStreamExtractor(out, func(p string) string {
		payloads = append(payloads, p)
		return "ok"
	})

	extractor.Feed("<action>update_requirements|topic:math</action>")
	extractor.Feed("<action>require_confirm</action>")
	extractor.Flush()

	_ = collectChunks(extractor, out)

	assert.Equal(t, []string{"update_requirements|topic:math", "require_confirm"}, payloads)
}

func TestStreamExtractorHistoryPlainText(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, nil)

	extractor.Feed("hello world")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "hello world", extractor.history.String())
}

func TestStreamExtractorHistorySingleAction(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string { return "all fields are updated" })

	extractor.Feed("ok ")
	extractor.Feed("<action>")
	extractor.Feed("update_requirements|topic:math")
	extractor.Feed("</action>")
	extractor.Feed(" done")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "ok <action>update_requirements|topic:math</action> done", extractor.history.String())
	assert.Equal(t, []string{"all fields are updated"}, extractor.toolResults)
	assert.Equal(t, []string{"voice-agent-action-0"}, extractor.toolCallIDs)
}

func TestStreamExtractorHistoryMultipleActions(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string {
		if p == "a1" {
			return "result1"
		}
		return "result2"
	})

	extractor.Feed("<action>a1</action> mid <action>a2</action>")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "<action>a1</action> mid <action>a2</action>", extractor.history.String())
	assert.Equal(t, []string{"result1", "result2"}, extractor.toolResults)
	assert.Equal(t, []string{"voice-agent-action-0", "voice-agent-action-1"}, extractor.toolCallIDs)
}

func TestStreamExtractorHistorySplitActionTag(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, func(p string) string { return "done" })

	extractor.Feed("hello <act")
	extractor.Feed("ion>data</action>")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "hello <action>data</action>", extractor.history.String())
	assert.Equal(t, []string{"done"}, extractor.toolResults)
	assert.Equal(t, []string{"voice-agent-action-0"}, extractor.toolCallIDs)
}

func TestStreamExtractorHistoryUnclosedAction(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out, nil)

	extractor.Feed("text <action>unclosed")
	extractor.Flush()

	_ = collectChunks(extractor, out)
	assert.Equal(t, "text <action>unclosed", extractor.history.String())
}
