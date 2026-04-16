package service

import (
	"context"
	"fmt"
	"strings"

	"educationagent/internal/model"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
	"educationagent/internal/voiceagent"

	"github.com/openai/openai-go/v3"
)

// VoiceAgentService drives the finetuned voice agent LLM and streams the response.
type VoiceAgentService interface {
	// StreamTurn runs the voice agent on the user transcript and emits SSE chunks.
	// needsInterruptedPrefix tells the backend whether to prepend </interrupted> to
	// the user message. It is determined by the frontend based on TTS playback state.
	// interruptedAssistant contains the truncated assistant text from a previous turn
	// that was interrupted during TTS playback. The backend appends it to history
	// before starting the new inference so the LLM context stays consistent.
	StreamTurn(ctx context.Context, st *state.AppState, transcript string, needsInterruptedPrefix bool, interruptedAssistant string, out chan<- model.SSEChunk) error
}

// DefaultVoiceAgentService uses an LLM to generate the voice turn.
type DefaultVoiceAgentService struct {
	agent    *toolcalling.Agent
	executor *voiceagent.Executor
}

// NewVoiceAgentService creates the voice agent from environment config.
func NewVoiceAgentService(cfg toolcalling.LLMConfig, exec *voiceagent.Executor) VoiceAgentService {
	return &DefaultVoiceAgentService{
		agent:    toolcalling.NewAgent(cfg),
		executor: exec,
	}
}

// StreamTurn builds the context, calls the LLM stream, parses inline actions,
// forwards SSE chunks to out, and executes actions via the executor.
// Action results are appended to voice history after the turn ends so the next
// LLM turn can observe them.
func (s *DefaultVoiceAgentService) StreamTurn(ctx context.Context, st *state.AppState, transcript string, needsInterruptedPrefix bool, interruptedAssistant string, out chan<- model.SSEChunk) error {
	defer close(out)

	// If the frontend interrupted an assistant turn during TTS playback, append
	// the truncated spoken text to history so the backend context stays in sync.
	if interruptedAssistant != "" {
		st.AppendVoiceHistory(openai.ChatCompletionMessageParamUnion{
			OfAssistant: &openai.ChatCompletionAssistantMessageParam{
				Content: openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(interruptedAssistant),
				},
			},
		})
	}

	queueStatus := "empty"
	if _, ok := st.PeekPPTMessageQueue(); ok {
		queueStatus = "not empty"
	}

	userContent := fmt.Sprintf("<status>%s</status>\n<user>%s</user>", queueStatus, transcript)
	if needsInterruptedPrefix {
		userContent = "</interrupted>\n" + userContent
	}

	// Emit the fully formatted user message first so the frontend can append it
	// to the conversation history before the assistant turn starts.
	out <- model.SSEChunk{Type: "user_transcript", Text: userContent}

	sys := openai.SystemMessage(`You are a helpful voice assistant for a PPT generation app.
Talk naturally with the user.
When you need to perform an action, emit it using the exact XML-like tag format:
<action>tool_name|param1:value1|param2:value2</action>
Available actions:
- update_requirements|topic:...|style:...|total_pages:...|audience:...
- require_confirm
- send_to_ppt_agent|data:...
- fetch_from_ppt_message_queue
Do not output any explanation inside the action tag. Keep the tag concise.`)

	// -------------------------------------------------------------------------
	// Round 1: assistant generates TTS + action(s)
	// -------------------------------------------------------------------------
	history := st.GetVoiceHistory()
	history = append(history, openai.UserMessage(userContent))
	messages := append([]openai.ChatCompletionMessageParamUnion{sys}, history...)
	stream := s.agent.StreamChat(ctx, messages)

	extractor := newStreamExtractor(out, func(payload string) string {
		if s.executor == nil {
			return "no executor registered"
		}
		res, err := s.executor.Execute(ctx, payload)
		if err != nil && res == "" {
			res = err.Error()
		}
		return res
	})

	for token := range stream {
		extractor.Feed(token)
	}
	extractor.Flush()

	if ctx.Err() != nil {
		// Turn was interrupted; do not persist the partial assistant message.
		// The frontend owns history reconstruction for interrupted turns.
		return ctx.Err()
	}

	// Persist round 1: user -> assistant -> tool(s)
	st.AppendVoiceHistory(openai.UserMessage(userContent))
	if assistantContent := extractor.history.String(); assistantContent != "" {
		st.AppendVoiceHistory(openai.ChatCompletionMessageParamUnion{
			OfAssistant: &openai.ChatCompletionAssistantMessageParam{
				Content: openai.ChatCompletionAssistantMessageParamContentUnion{
					OfString: openai.String(assistantContent),
				},
			},
		})
	}
	for _, tr := range extractor.toolResults {
		st.AppendVoiceHistory(openai.ToolMessage(tr, "voice-agent-action"))
	}

	// -------------------------------------------------------------------------
	// Round 2 (conditional): if any action is fetch_from_ppt_message_queue,
	// run a second inference so the model can report the tool results.
	// -------------------------------------------------------------------------
	hasFetch := false
	for _, a := range extractor.actions {
		if strings.HasPrefix(a, "fetch_from_ppt_message_queue") {
			hasFetch = true
			break
		}
	}

	if hasFetch {
		history = st.GetVoiceHistory()
		messages = append([]openai.ChatCompletionMessageParamUnion{sys}, history...)
		stream2 := s.agent.StreamChat(ctx, messages)

		var secondAssistant strings.Builder
		for token := range stream2 {
			if ctx.Err() != nil {
				break
			}
			secondAssistant.WriteString(token)
			out <- model.SSEChunk{Type: "tts", Text: token}
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if content := secondAssistant.String(); content != "" {
			st.AppendVoiceHistory(openai.ChatCompletionMessageParamUnion{
				OfAssistant: &openai.ChatCompletionAssistantMessageParam{
					Content: openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: openai.String(content),
					},
				},
			})
		}
	}

	// Emit turn_end.
	select {
	case out <- model.SSEChunk{Type: "turn_end"}:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// streamExtractor parses inline <action>...</action> tags from a token stream
// and emits model.SSEChunk values. When a complete action is found, onAction
// is invoked and its return value is emitted as a "tool" chunk.
type streamExtractor struct {
	out         chan<- model.SSEChunk
	raw         strings.Builder
	history     strings.Builder
	actions     []string
	toolResults []string
	inAction    bool
	onAction    func(payload string) string
}

func newStreamExtractor(out chan<- model.SSEChunk, onAction func(string) string) *streamExtractor {
	return &streamExtractor{out: out, onAction: onAction}
}

func (e *streamExtractor) emit(chunk model.SSEChunk) {
	select {
	case e.out <- chunk:
	default:
	}
}

func (e *streamExtractor) writeText(text string) {
	if text != "" {
		e.emit(model.SSEChunk{Type: "tts", Text: text})
		e.history.WriteString(text)
	}
}

func (e *streamExtractor) writeAction(payload string) {
	e.emit(model.SSEChunk{Type: "action", Payload: payload})
	e.actions = append(e.actions, payload)
	e.history.WriteString("<action>")
	e.history.WriteString(payload)
	e.history.WriteString("</action>")
	if e.onAction != nil {
		toolText := e.onAction(payload)
		if toolText != "" {
			e.emit(model.SSEChunk{Type: "tool", Text: toolText})
			e.toolResults = append(e.toolResults, toolText)
		}
	}
}

// Feed processes one token (which may contain multiple characters).
func (e *streamExtractor) Feed(token string) {
	e.raw.WriteString(token)
	for {
		s := e.raw.String()
		if e.inAction {
			idx := strings.Index(s, "</action>")
			if idx >= 0 {
				payload := s[:idx]
				e.writeAction(payload)
				e.raw.Reset()
				e.raw.WriteString(s[idx+9:])
				e.inAction = false
				continue
			}
			break
		}

		idx := strings.Index(s, "<action>")
		if idx >= 0 {
			text := s[:idx]
			e.writeText(text)
			e.raw.Reset()
			e.raw.WriteString(s[idx+8:])
			e.inAction = true
			continue
		}

		// Safety flush: <action> is 8 chars. If the trailing 8 chars do not contain '<',
		// no action tag can cross the boundary, so everything before them is safe to emit.
		if len(s) > 8 {
			suffix := s[len(s)-8:]
			if !strings.Contains(suffix, "<") {
				e.writeText(s[:len(s)-8])
				e.raw.Reset()
				e.raw.WriteString(suffix)
			}
		}
		break
	}
}

// Flush drains any remaining text when the stream ends.
func (e *streamExtractor) Flush() {
	s := e.raw.String()
	if s == "" {
		return
	}
	if e.inAction {
		// Unclosed action at EOF: treat it as plain text to avoid losing content.
		e.writeText("<action>" + s)
	} else {
		e.writeText(s)
	}
}
