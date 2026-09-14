package llm

import (
	"context"
	"io"
	"net/http"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

// Event is one streaming event. Kind decides which field is meaningful.
type Event struct {
	Kind         EventKind
	Text         string       // KindContent: raw delta.content
	ToolCall     ToolCallPart // KindToolCall: one tool_calls delta fragment
	FinishReason string       // KindFinish: stop / tool_calls / length ...
}

type EventKind int

const (
	EventContent  EventKind = iota // delta.content is non-empty
	EventToolCall                  // delta.tool_calls appeared
	EventFinish                    // finish_reason arrived; last event before the channel closes
)

// ToolCallPart is an incremental update for one slot of the tool_calls
// array. Index selects the slot; ID and Name usually arrive on the first
// fragment only; Arguments is an append-only JSON fragment.
type ToolCallPart struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// chunkStream is the slice of *ssestream.Stream[openai.ChatCompletionChunk]
// that the event loop needs. Declared as an interface so tests can feed
// hand-written SSE text through streamFromReader.
type chunkStream interface {
	Next() bool
	Current() openai.ChatCompletionChunk
	Err() error
	Close() error
}

// streamFromReader decodes raw SSE bytes (e.g. a recorded session) through
// the SDK's own decoder, so fixtures exercise the same code path as live
// traffic.
func streamFromReader(r io.Reader) chunkStream {
	res := &http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(r),
	}
	return ssestream.NewStream[openai.ChatCompletionChunk](ssestream.NewDecoder(res), nil)
}

// eventsFromStream converts a chunk stream into events. The channel is
// closed when the stream ends ([DONE]) or ctx is cancelled. A mid-stream
// failure surfaces as a final Event{Kind: EventFinish, FinishReason: "error"}
// carrying the message in Text, because the stream is already established
// and no error return value exists at that point.
func eventsFromStream(ctx context.Context, stream chunkStream) <-chan Event {
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)
		defer stream.Close()

		for stream.Next() {
			for _, ev := range chunkToEvents(stream.Current()) {
				select {
				case ch <- ev:
				case <-ctx.Done():
					return
				}
			}
		}

		if err := stream.Err(); err != nil && ctx.Err() == nil {
			// Cancellation is an interruption, not an error: stay silent.
			select {
			case ch <- Event{Kind: EventFinish, FinishReason: "error", Text: err.Error()}:
			case <-ctx.Done():
			}
		}
	}()

	return ch
}

func chunkToEvents(chunk openai.ChatCompletionChunk) []Event {
	var events []Event
	for _, choice := range chunk.Choices {
		delta := choice.Delta
		// The first frame carries role + empty content; forwarding the empty
		// string would leak a bogus token into the TTS pipeline.
		if delta.Content != "" {
			events = append(events, Event{Kind: EventContent, Text: delta.Content})
		}
		for _, tc := range delta.ToolCalls {
			events = append(events, Event{
				Kind: EventToolCall,
				ToolCall: ToolCallPart{
					Index:     int(tc.Index),
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
		if choice.FinishReason != "" {
			events = append(events, Event{Kind: EventFinish, FinishReason: choice.FinishReason})
		}
	}
	return events
}
