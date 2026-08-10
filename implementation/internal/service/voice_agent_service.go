package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"educationagent/internal/model"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
	"educationagent/internal/voiceagent"

	"github.com/openai/openai-go/v3"
)

// Voice history compression. A sliding tail window shifts the context prefix
// every turn (breaking KV-cache reuse), can split message pairs, and silently
// drops task parameters. Instead, once the stored history grows past
// voiceCompressThreshold, the oldest chunk is compressed into a rolling
// summary. The summary is frozen between compression events and injected
// right after the system prompt, so the context prefix stays stable and
// append-only — server-side prefix caching (vLLM APC / SGLang RadixAttention)
// can hit on every turn, missing only on rare compression events.
const (
	// voiceCompressThreshold triggers compression when the stored voice
	// history exceeds this many messages.
	voiceCompressThreshold = 24
	// voiceCompressKeepRecent is how many recent messages stay verbatim.
	voiceCompressKeepRecent = 12
)

// voiceSummaryPrompt rolls the previous summary and a chunk of old history
// into a new compact summary.
const voiceSummaryPrompt = `你是对话压缩器。把"此前的摘要"和"一段较旧的对话历史"合并压缩为一段新摘要，要求：
- 保留任务关键参数（物块颜色、目标坐标）、已完成的进展、未完成事项、用户的明确偏好；
- 丢弃寒暄、重复内容与已无关紧要的中间过程；
- 150 字以内，只输出摘要文本。`

// splitCompressionWindow splits history into the old chunk to compress and
// the recent window to keep verbatim. Leading stale snapshots of the recent
// window (a <queue_status> status bar or a tool result whose paired message
// was compressed away) are moved into the old chunk so no pair is split.
// Returns (nil, history) when compression is not yet needed.
func splitCompressionWindow(history []openai.ChatCompletionMessageParamUnion, threshold, keepRecent int) (old, recent []openai.ChatCompletionMessageParamUnion) {
	if len(history) <= threshold {
		return nil, history
	}
	cut := len(history) - keepRecent
	for cut < len(history) {
		m := history[cut]
		if m.OfTool != nil {
			cut++
			continue
		}
		if u := m.OfUser; u != nil && u.Content.OfString.Valid() &&
			strings.HasPrefix(u.Content.OfString.Value, "<queue_status>") {
			cut++
			continue
		}
		break
	}
	return history[:cut], history[cut:]
}

// renderHistoryForSummary renders messages as "role: text" lines for the
// compression prompt.
func renderHistoryForSummary(msgs []openai.ChatCompletionMessageParamUnion) string {
	var b strings.Builder
	for _, m := range msgs {
		switch {
		case m.OfUser != nil && m.OfUser.Content.OfString.Valid():
			b.WriteString("user: " + m.OfUser.Content.OfString.Value + "\n")
		case m.OfAssistant != nil && m.OfAssistant.Content.OfString.Valid():
			b.WriteString("assistant: " + m.OfAssistant.Content.OfString.Value + "\n")
		case m.OfTool != nil && m.OfTool.Content.OfString.Valid():
			b.WriteString("tool: " + m.OfTool.Content.OfString.Value + "\n")
		}
	}
	return b.String()
}

// compressVoiceHistoryIfNeeded compresses older voice turns into the rolling
// summary when the stored history exceeds voiceCompressThreshold. Compression
// failure is non-fatal: the history is left untouched and compression is
// retried on a later turn; if the history grows past twice the threshold the
// oldest chunk is hard-dropped as a safety valve.
func (s *DefaultVoiceAgentService) compressVoiceHistoryIfNeeded(ctx context.Context, st *state.AppState) {
	history := st.GetVoiceHistory()
	old, recent := splitCompressionWindow(history, voiceCompressThreshold, voiceCompressKeepRecent)
	if old == nil {
		return
	}

	previous := st.GetVoiceSummary()
	if previous == "" {
		previous = "无"
	}
	summary, err := s.agent.ChatText(ctx, []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(voiceSummaryPrompt),
		openai.UserMessage(fmt.Sprintf("此前的摘要：\n%s\n\n较旧的对话历史：\n%s", previous, renderHistoryForSummary(old))),
	})
	if err != nil || strings.TrimSpace(summary) == "" {
		if len(history) > 2*voiceCompressThreshold {
			st.SetVoiceHistory(recent) // safety valve against runaway growth
		}
		return
	}
	st.SetVoiceSummary(strings.TrimSpace(summary))
	st.SetVoiceHistory(recent)
}

// buildVoiceMessages assembles the inference context: system prompt, the
// frozen rolling summary (if any), then the verbatim recent history, then any
// new messages for this turn.
func buildVoiceMessages(sys openai.ChatCompletionMessageParamUnion, st *state.AppState, extra ...openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	messages := []openai.ChatCompletionMessageParamUnion{sys}
	if summary := st.GetVoiceSummary(); summary != "" {
		messages = append(messages, openai.UserMessage("【此前对话摘要】"+summary))
	}
	messages = append(messages, st.GetVoiceHistory()...)
	messages = append(messages, extra...)
	return messages
}

// voiceSystemPrompt is the Voice Agent's only system prompt. It is
// byte-identical to the system message embedded in the fine-tuning data
// (训练数据/voice_agent), so the inference-time context prefix matches
// training. Tools are declared via the API tools field (voiceToolSchemas),
// not taught in the prompt. Unlike the old PPT system there is no
// remember/require_confirm pipeline: the human speaks tasks directly, intent
// is clarified in dialogue, and once clear the task is forwarded to the arm
// agent (async_dual_agent_system_design.md §5).
const voiceSystemPrompt = `你是异步双 agent 系统中的 Voice Agent，是人唯一的交互入口：你与人实时语音对话，理解任务意图，把任务下发给后台操作机械臂的 Arm Agent，并把 Arm Agent 上报的进度/结果语音转述给人。Arm Agent 不直接对人输出。

铁律：
1. 每条人类 user 消息之后紧跟一条独立的 role=user 状态栏消息 <queue_status>empty/not empty</queue_status>，反映 Arm Agent 是否有未读消息；当工具结果、人类输入、状态栏同时出现时，顺序固定为：工具结果 → 人类输入 → 状态栏。
2. 每轮回复必须先输出自然口语，再调用工具；本轮无需工具时只输出纯口语。
3. 人的意图缺关键信息（抓什么颜色的物块、放到哪里）时，通过对话逐项问清，确认意图完整后再 send_to_arm_agent；content 要完整转述任务（动作+颜色+坐标）。
4. 仅当状态栏消息为 <queue_status>not empty</queue_status> 时才调用 get_message_from_arm_agent；empty 时不要调用。状态栏为 empty 且人催进度时，如实说"还没有新进展，有消息我第一时间告诉你"，不得谎称完成。
5. 若 user 消息以 </interrupted> 开头，表示人在你上一轮播报过程中打断了你，自然地回应新输入即可，不要重复已说内容。
6. 回复要口语化、简洁，适合语音播报；工具执行是静默的，即使用户在工具执行期间说话，工具仍会在后台完整执行完毕。`

// voiceToolSchemas declares the two voice tools (finetuning_of_voice_agent.md
// §2). They are registered on the LLM agent so the chat template renders them
// into the system block's <tools> section — the same shape the model saw
// during fine-tuning. Execution stays inline (streamExtractor + Executor), so
// Function is nil here.
var voiceToolSchemas = []toolcalling.Tool{
	{
		Name:        "send_to_arm_agent",
		Description: "把任务/变更/取消消息发给后台操作机械臂的 Arm Agent，返回\"发送成功\"。红线：人的任务意图不明确（缺颜色、缺位置等关键信息，或只是在闲聊/问进度）时不得调用，先通过对话问清楚再下发。",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"content": map[string]any{"type": "string", "description": "完整转述的任务内容（动作+颜色+坐标），并注明完成后通过 send_to_voice_agent() 将结果返回给 voice agent"},
		}, "required": []string{"content"}},
	},
	{
		Name:        "get_message_from_arm_agent",
		Description: "一次性消费 Arm Agent 上报的全部消息，返回 all_messages_from_arm_agent:消息1;消息2 或 当前没有新消息。仅当状态栏消息为 <queue_status>not empty</queue_status> 时才调用；empty 时不要调用。",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	},
}

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
	agent := toolcalling.NewAgent(cfg)
	for _, schema := range voiceToolSchemas {
		agent.AddTool(schema)
	}
	return &DefaultVoiceAgentService{
		agent:    agent,
		executor: exec,
	}
}

// StreamTurn builds the context, calls the LLM stream, parses inline
// <tool_call> tags (Qwen3 native JSON payload), forwards SSE chunks to out,
// and executes tool calls via the executor. Tool results are appended to
// voice history after the turn ends as single role=tool messages (the chat
// template wraps them into the user <tool_response> block — design §5) so
// the next LLM turn can observe them.
func (s *DefaultVoiceAgentService) StreamTurn(ctx context.Context, st *state.AppState, transcript string, needsInterruptedPrefix bool, interruptedAssistant string, out chan<- model.SSEChunk) error {
	defer close(out)

	// If the frontend interrupted an assistant turn during TTS playback, append
	// the truncated spoken text to history so the backend context stays in sync.
	if interruptedAssistant != "" {
		st.AppendVoiceHistory(assistantTextMessage(interruptedAssistant))
	}

	// Compress older turns into the rolling summary if the history outgrew
	// the threshold; between compression events the context prefix stays
	// stable so server-side prefix caching can hit.
	s.compressVoiceHistoryIfNeeded(ctx, st)

	// Every human user message enters the context followed by a separate
	// role=user <queue_status> status bar message (design §4.3), reflecting
	// message_from_arm_agent_queue. When a tool response, the human input
	// and the status bar are all present, their order is fixed:
	// tool response → user input → status bar.
	queueStatus := "empty"
	if _, ok := st.PeekArmMessageQueue(); ok {
		queueStatus = "not empty"
	}

	userContent := transcript
	if needsInterruptedPrefix {
		userContent = "</interrupted>\n" + userContent
	}
	statusBar := fmt.Sprintf("<queue_status>%s</queue_status>", queueStatus)

	// Emit the fully formatted user message and the status bar first so the
	// frontend can append them to the conversation history before the
	// assistant turn starts.
	out <- model.SSEChunk{Type: "user_transcript", Text: userContent}
	out <- model.SSEChunk{Type: "user_transcript", Text: statusBar}

	sys := openai.SystemMessage(voiceSystemPrompt)

	// -------------------------------------------------------------------------
	// Round 1: assistant generates TTS + tool call(s)
	// -------------------------------------------------------------------------
	messages := buildVoiceMessages(sys, st, openai.UserMessage(userContent), openai.UserMessage(statusBar))
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

	// Persist round 1: user -> status bar -> assistant -> tool result(s) as
	// single role=tool messages (design §5: the chat template wraps them into
	// the user <tool_response> block, so no manual dual-role writeback).
	st.AppendVoiceHistory(openai.UserMessage(userContent))
	st.AppendVoiceHistory(openai.UserMessage(statusBar))
	if assistantContent := extractor.history.String(); assistantContent != "" {
		st.AppendVoiceHistory(assistantTextMessage(assistantContent))
	}
	for _, tr := range extractor.toolResults {
		st.AppendVoiceHistory(openai.ToolMessage(tr, "voice-agent-tool"))
	}

	// -------------------------------------------------------------------------
	// Round 2 (conditional): if the tool call was get_message_from_arm_agent,
	// run a second inference so the model can report the consumed messages.
	// -------------------------------------------------------------------------
	hasFetch := false
	for _, a := range extractor.actions {
		name, _, err := voiceagent.ParseToolCall(a)
		if err == nil && name == "get_message_from_arm_agent" {
			hasFetch = true
			break
		}
	}

	if hasFetch {
		messages = buildVoiceMessages(sys, st)
		stream2 := s.agent.StreamChat(ctx, messages)

		extractor2 := newStreamExtractor(out, func(payload string) string {
			if s.executor == nil {
				return "no executor registered"
			}
			res, err := s.executor.Execute(ctx, payload)
			if err != nil && res == "" {
				res = err.Error()
			}
			return res
		})
		for token := range stream2 {
			if ctx.Err() != nil {
				break
			}
			extractor2.Feed(token)
		}
		extractor2.Flush()

		if ctx.Err() != nil {
			return ctx.Err()
		}
		if content := extractor2.history.String(); content != "" {
			st.AppendVoiceHistory(assistantTextMessage(content))
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

// streamExtractor parses inline <tool_call>...</tool_call> tags from a token
// stream and emits model.SSEChunk values. When a complete tool call is found,
// onAction is invoked and its return value is emitted as a "tool" chunk.
type streamExtractor struct {
	out         chan<- model.SSEChunk
	raw         strings.Builder
	history     strings.Builder
	actions     []string
	toolResults []string
	inToolCall  bool
	onAction    func(payload string) string
	utf8Buf     []byte
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
	if text == "" {
		return
	}
	// Buffer incomplete UTF-8 sequences across token boundaries.
	data := append(e.utf8Buf, []byte(text)...)
	// Find the last valid UTF-8 boundary.
	valid := len(data)
	for valid > 0 && !utf8.Valid(data[:valid]) {
		valid--
	}
	e.utf8Buf = data[valid:]
	if valid == 0 {
		return
	}
	safe := string(data[:valid])
	e.emit(model.SSEChunk{Type: "tts", Text: safe})
	e.history.WriteString(safe)
}

func (e *streamExtractor) writeToolCall(payload string) {
	e.emit(model.SSEChunk{Type: "action", Payload: payload})
	e.actions = append(e.actions, payload)
	e.history.WriteString(toolcalling.ToolCallOpenTag)
	e.history.WriteString(payload)
	e.history.WriteString(toolcalling.ToolCallCloseTag)
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
		if e.inToolCall {
			idx := strings.Index(s, toolcalling.ToolCallCloseTag)
			if idx >= 0 {
				payload := s[:idx]
				e.writeToolCall(payload)
				e.raw.Reset()
				e.raw.WriteString(s[idx+len(toolcalling.ToolCallCloseTag):])
				e.inToolCall = false
				continue
			}
			break
		}

		idx := strings.Index(s, toolcalling.ToolCallOpenTag)
		if idx >= 0 {
			text := s[:idx]
			e.writeText(text)
			e.raw.Reset()
			e.raw.WriteString(s[idx+len(toolcalling.ToolCallOpenTag):])
			e.inToolCall = true
			continue
		}

		// Safety flush: <tool_call> is 11 chars. If the trailing 11 chars do not
		// contain '<', no tool_call tag can cross the boundary, so everything
		// before them is safe to emit.
		if len(s) > len(toolcalling.ToolCallOpenTag) {
			suffix := s[len(s)-len(toolcalling.ToolCallOpenTag):]
			if !strings.Contains(suffix, "<") {
				e.writeText(s[:len(s)-len(toolcalling.ToolCallOpenTag)])
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
	if e.inToolCall {
		e.writeText(toolcalling.ToolCallOpenTag + s)
	} else if s != "" {
		e.writeText(s)
	}
	// Flush any remaining incomplete UTF-8 bytes as-is.
	if len(e.utf8Buf) > 0 {
		safe := string(e.utf8Buf)
		e.emit(model.SSEChunk{Type: "tts", Text: safe})
		e.history.WriteString(safe)
		e.utf8Buf = nil
	}
}
