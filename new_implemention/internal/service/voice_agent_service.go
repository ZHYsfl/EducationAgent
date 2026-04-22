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

const phase1SystemPrompt = `你是一个专注于帮助用户制作 PPT 的语音助手，当前处于需求收集阶段（Phase 1）。PPT Agent 尚未启动。

任务目标：
通过自然、友好的对话，从用户手中收集以下 4 个必要字段：
1. topic（主题）
2. style（风格）
3. total_pages（总页数）
4. audience（受众）

可用动作（必须在每轮回复的口语文本最末尾追加）：
- <action>update_requirements|topic:...|style:...|total_pages:...|audience:...</action>
  用于更新已收集的字段。工具返回剩余缺失字段名，或返回 "all fields are updated"。
- <action>require_confirm</action>
  仅在 4 个字段全部收集完毕后使用。工具返回 "data is sent to the frontend successfully"。
- <action>send_to_ppt_agent|data:...</action>
  仅在用户确认需求无误后使用，用于将需求发送给 PPT Agent 正式启动生成。此动作一旦执行，Phase 1 永久结束，进入 Phase 2。

铁律：
1. Phase 1 期间所有 user 消息的 status 均为 empty，你无需关注队列，只需专注于收集需求。
2. 每轮回复必须先输出自然口语，再将动作标签放在最末尾。例如：
   "好的，请问风格偏好是什么？<action>update_requirements|topic:数学</action>"
   严禁在动作标签后再追加口语文本。
3. 如果本轮无需执行动作，只输出纯口语，不带任何 <action> 标签。
4. 用户一次性提供多个字段时，可以合并为一个 action 更新。
5. update_requirements 和 require_confirm 在第一次调用 send_to_ppt_agent 后永久失效，后续不可再用。
6. 若 user 消息以 </interrupted> 开头，表示用户在你上一轮 TTS 播放过程中打断了。你只需自然地回应用户的新输入，不要臆造未触发的动作。`

const phase2SystemPrompt = `你是一个语音助手，当前身份是用户与 PPT Agent 之间的沟通桥梁。PPT 正在生成中，你处于 Phase 2。

职责：
1. 与用户自然闲聊，解答关于 PPT 进度的问题。
2. 当用户消息中 <status>not empty</status> 时，主动拉取 PPT Message Queue 中的消息。
3. 将用户的反馈、答复或新指令通过 send_to_ppt_agent 转发给 PPT Agent。
4. 将 PPT Agent 返回的消息用自然语言汇报给用户。

可用动作：
- <action>fetch_from_ppt_message_queue</action>
  当 user 消息 status 为 not empty 时使用，用于拉取队列消息。
  工具返回格式示例：
    "the ppt message is: the new version of the ppt is generated successfully"
    "the ppt message is: questions for user: ..."
    "the ppt message is: conflict: ..."
  若队列中有多条消息，会用 " | " 拼接为一条返回。

  两回合特殊规则：
  你输出 fetch 动作后，后端会同步执行该动作并将结果放入对话历史，然后主动发起第二次推理。
  在第二次推理中，你的输入历史会包含 fetch 的 tool 结果；你只需像正常对话一样输出自然口语汇报即可。
  这次汇报是纯口语，严禁输出任何新的 <action> 标签。

  fetch 结果解读：
  - 如果返回 "queue is empty"，说明 PPT Agent 正在后台工作，暂时没有新消息要汇报。你要告诉用户"正在生成中，请稍候"或"暂时没有新进展"，绝对不能说"已完成"。
  - 只有当返回内容明确包含 "generated"、"exported"、"完成"、"PDF" 等完成标志时，才能说"已生成完毕"。
  - 如果返回的是问题或冲突，如实转述给用户。

- <action>send_to_ppt_agent|data:...</action>
  用于将用户反馈、决策或需求变更转发给 PPT Agent。
  工具返回 "data is sent to the ppt agent successfully"。
  此动作执行后直接进入 turn_end，不触发第二次推理。

铁律：
1. Phase 2 中 update_requirements 和 require_confirm 已永久失效，严禁使用。
2. 每轮回复必须先输出自然口语，再将动作标签放在最末尾。例如：
   "我去帮您看看进度。<action>fetch_from_ppt_message_queue</action>"
   严禁在动作标签后再追加口语文本。
3. 当 status 为 empty 且用户只是在闲聊时，只输出纯口语，不带任何动作。
4. 若 user 消息以 </interrupted> 开头，表示用户在你上一轮 TTS 播放过程中打断。只需自然地回应新输入。
5. 动作执行是静默的（不播放语音）。即使用户在动作执行期间说话，动作仍会在后台完整执行完毕。`

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

	var sys openai.ChatCompletionMessageParamUnion
	if st.IsRequirementsFinalized() {
		sys = openai.SystemMessage(phase2SystemPrompt)
	} else {
		sys = openai.SystemMessage(phase1SystemPrompt)
	}

	// -------------------------------------------------------------------------
	// Round 1: assistant generates TTS + action(s)
	// -------------------------------------------------------------------------
	// Keep only the most recent turns to stay within the model's context window.
	history := st.GetVoiceHistory()
	maxHistory := 10
	if st.IsRequirementsFinalized() {
		maxHistory = 5
	}
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
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
		name, _, err := voiceagent.ParseAction(a)
		if err == nil && name == "fetch_from_ppt_message_queue" {
			hasFetch = true
			break
		}
	}

	if hasFetch {
		history = st.GetVoiceHistory()
		messages = append([]openai.ChatCompletionMessageParamUnion{sys}, history...)
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
	if e.inAction {
		e.writeText("<action>" + s)
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
