package service

import (
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
	extractor := newStreamExtractor(out)

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
	extractor := newStreamExtractor(out)

	extractor.Feed("ok ")
	extractor.Feed("<action>")
	extractor.Feed("update_requirements|topic:math")
	extractor.Feed("</action>")
	extractor.Feed(" done")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 3)
	require.Equal("tts", chunks[0].Type)
	require.Equal("ok ", chunks[0].Text)
	require.Equal("action", chunks[1].Type)
	require.Equal("update_requirements|topic:math", chunks[1].Payload)
	require.Equal("tts", chunks[2].Type)
	require.Equal(" done", chunks[2].Text)
}

func TestStreamExtractorSplitActionTag(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out)

	// <action> split across tokens
	extractor.Feed("hello <act")
	extractor.Feed("ion>data</action>")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 2)
	require.Equal("tts", chunks[0].Type)
	require.Equal("hello ", chunks[0].Text)
	require.Equal("action", chunks[1].Type)
	require.Equal("data", chunks[1].Payload)
}

func TestStreamExtractorUnclosedAction(t *testing.T) {
	out := make(chan model.SSEChunk, 10)
	extractor := newStreamExtractor(out)

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
	extractor := newStreamExtractor(out)

	extractor.Feed("<action>a1</action> mid <action>a2</action>")
	extractor.Flush()

	chunks := collectChunks(extractor, out)

	require := assert.New(t)
	require.GreaterOrEqual(len(chunks), 3)
	require.Equal("action", chunks[0].Type)
	require.Equal("a1", chunks[0].Payload)
	require.Equal("tts", chunks[1].Type)
	require.Equal(" mid ", chunks[1].Text)
	require.Equal("action", chunks[2].Type)
	require.Equal("a2", chunks[2].Payload)
}
