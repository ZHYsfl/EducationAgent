package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
	"educationagent/internal/tools"

	"github.com/openai/openai-go/v3"
)

// armMaxToolCallsPerTurn caps the tool-call loop inside one runtime turn so a
// misbehaving model cannot spin forever.
const armMaxToolCallsPerTurn = 32

// armSystemPrompt is the Arm Agent's system prompt. It mirrors the fine-tuning
// data contract (finetuning_of_arm_agent.md): compact <tool_call> format,
// tool/user dual-role writeback, and the <queue_status> status bar.
const armSystemPrompt = `你是异步双 agent 系统中的 Arm Agent，在后台操作机械臂真机完成具身任务。你不与人直接对话：你的输入是任务消息（all_messages_from_voice_agent:...）、工具结果和 <queue_status> 状态栏；你对外界的一切反馈都通过 send_to_voice_agent 上报，由 Voice Agent 语音转述给人。

可用工具（在回复中以如下紧凑格式调用，每次调用一个）：
<tool_call>
工具名:参数
</tool_call>

1. get_current_coordinates（无参数）
   获取当前坐标。返回当前坐标字符串（如"我的坐标是1.0,2.0,3.0"）。
2. move_to_coordinates:x,y,z
   移动到指定坐标。返回"成功到达{x},{y},{z}"或"未到达{x},{y},{z}，误差是{error}"。
3. grab_the_block:color
   抓取指定颜色物块。合法颜色只有 yellow、red、white。返回只有三种："没有这种颜色的物块，无法夹取"、"有这种颜色的物块，且夹取物块成功"、"有这种颜色的物块，但夹取物块失败"。
4. release_the_block（无参数）
   放下当前抓取的物块。返回只有三种："本身就没加起来物块"、"成功释放物块"、"释放物块失败，请用手直接拿出来物块"。
5. send_to_voice_agent:content
   把进度/结果/求助消息上报给 Voice Agent。返回"发送成功"。
6. get_message_from_voice_agent（无参数）
   一次性消费 Voice Agent 发来的全部消息。返回"all_messages_from_voice_agent:消息1;消息2"或"当前没有新消息"。

状态栏机制：每条工具结果之后会追加一条 role=user 的 <queue_status>empty/not empty</queue_status> 消息，反映是否有未读的新指令；当工具结果、user 输入、状态栏同时出现时，顺序固定为：工具结果 → user 输入 → 状态栏。看到 not empty 时，调用 get_message_from_voice_agent 主动消费（可能是改颜色/改位置/取消/追加任务），消费后调整执行；empty 时不要调用该工具。注意：你空闲时被自动注入的任务消息（all_messages_from_voice_agent:...）后面不带状态栏——队列刚被排空，直接开始执行即可。

行为准则：
- 标准任务链：get_current_coordinates → move_to_coordinates（物块位置）→ grab_the_block（指定颜色）→ move_to_coordinates（目标位置）→ release_the_block → send_to_voice_agent（完成汇报）。
- 工具失败时重试一次或调整策略；反复失败（尤其是"释放物块失败，请用手直接拿出来物块"）必须通过 send_to_voice_agent 求助，说清需要人做什么。
- 任务消息缺颜色、缺坐标等关键信息时，不许臆造参数，通过 send_to_voice_agent 反问，由 Voice Agent 回去问人。
- 变更颜色时若已夹起旧物块，先 release_the_block 再抓新颜色；收到取消指令时安全收尾（必要时保持夹持）并上报。
- 收到闲聊/无关消息时，说明自己只执行具身任务，不展开闲聊。
- 关键节点（开始执行、抓取结果、到达目标、任务完成、异常）都用 send_to_voice_agent 上报；汇报要说人话、信息完整（做了什么、结果如何、需要人做什么）。
- 工具返回字符串是事实，不得篡改或编造；参数（坐标、颜色）在同一次任务中前后一致。`

// ArmService manages the arm agent runtime, its tools, and queue interactions.
// It is the arm-agent counterpart of the old PPTService: the voice agent
// enqueues tasks via OnVoiceMessage; the background runtime consumes them and
// drives the finetuned arm LLM through the embodied tool chain.
type ArmService struct {
	state   *state.AppState
	runtime *state.AgentRuntime
	agent   *toolcalling.Agent
	gateway *tools.ArmGateway

	// runTurnFn is injectable for testing.
	runTurnFn func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error)
	runTurnMu sync.RWMutex
}

// NewArmService creates the arm service. If agent is nil, a default text-only
// LLM client is built from ARM_LLM_* env vars; if gateway is nil, one is built
// from ARM_GATEWAY_BASE_URL (default http://127.0.0.1:8000).
func NewArmService(st *state.AppState, agent *toolcalling.Agent, gateway *tools.ArmGateway) *ArmService {
	svc := &ArmService{
		state:   st,
		runtime: state.NewAgentRuntime(),
	}
	if agent != nil {
		svc.agent = agent
	} else {
		svc.agent = toolcalling.NewAgent(toolcalling.LLMConfig{
			APIKey:  os.Getenv("ARM_LLM_API_KEY"),
			Model:   os.Getenv("ARM_LLM_MODEL"),
			BaseURL: os.Getenv("ARM_LLM_BASE_URL"),
			ExtraBody: map[string]any{
				"chat_template_kwargs": map[string]any{"enable_thinking": false},
			},
		})
	}
	if gateway != nil {
		svc.gateway = gateway
	} else {
		base := strings.TrimSpace(os.Getenv("ARM_GATEWAY_BASE_URL"))
		if base == "" {
			base = "http://127.0.0.1:8000"
		}
		svc.gateway = tools.NewArmGateway(base)
	}
	svc.runTurnFn = svc.runTurn
	return svc
}

// SetRunTurnFn allows tests to override the turn loop.
func (s *ArmService) SetRunTurnFn(fn func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error)) {
	s.runTurnMu.Lock()
	defer s.runTurnMu.Unlock()
	s.runTurnFn = fn
}

// OnVoiceMessage handles every message sent from the voice agent via
// send_to_arm_agent. The message is always enqueued first; the runtime is
// (re)started only when idle, so a running arm agent is never interrupted
// mid-inference — it picks the message up via the <queue_status> status bar.
func (s *ArmService) OnVoiceMessage(content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("empty task content")
	}
	s.state.SendToArmAgent(content)
	if !s.runtime.IsRunning() {
		s.startRuntime()
	}
	return nil
}

// SendToVoiceAgent enqueues a message from the arm agent into
// message_from_arm_agent_queue (used by the REST test endpoint; the agent
// itself goes through the send_to_voice_agent tool).
func (s *ArmService) SendToVoiceAgent(content string) error {
	s.state.SendToVoiceAgent(content)
	return nil
}

// IsRuntimeRunning reports whether the arm agent goroutine is active.
func (s *ArmService) IsRuntimeRunning() bool {
	return s.runtime.IsRunning()
}

// StopRuntime cancels the arm agent runtime goroutine.
func (s *ArmService) StopRuntime() {
	s.runtime.Stop()
}

// WaitRuntime blocks until the arm agent runtime goroutine exits.
func (s *ArmService) WaitRuntime() {
	s.runtime.Wait()
}

// buildSystemMessage returns the (static) arm agent system message.
func (s *ArmService) buildSystemMessage() openai.ChatCompletionMessageParamUnion {
	return openai.SystemMessage(armSystemPrompt)
}

// startRuntime is the "idle auto-consume" entry point of the design (§4.3):
// when the arm agent is idle, the runtime drains message_from_voice_agent_queue
// and the consumed messages enter the context as one user message. No
// <queue_status> status bar is appended here — the queue was just drained, so
// the bar would always read "empty"; the bar only follows tool results while
// the agent is busy.
func (s *ArmService) startRuntime() {
	msgs := s.state.DrainVoiceMessageQueue()
	if len(msgs) == 0 {
		return
	}
	history := s.state.GetArmHistory()
	if len(history) == 0 || history[0].OfSystem == nil {
		history = append([]openai.ChatCompletionMessageParamUnion{s.buildSystemMessage()}, history...)
	}
	history = append(history, openai.UserMessage(consumeResultString(msgs)))
	s.state.SetArmHistory(history)
	s.runArmAgentLoop()
}

func (s *ArmService) runArmAgentLoop() {
	s.runtime.Start(func(ctx context.Context) {
		for {
			if ctx.Err() != nil {
				return
			}

			history := s.state.GetArmHistory()

			s.runTurnMu.RLock()
			fn := s.runTurnFn
			s.runTurnMu.RUnlock()

			msgs, err := fn(ctx, history)
			if err != nil {
				if ctx.Err() == nil {
					s.state.BroadcastArmLog("[error] arm agent turn: " + err.Error())
				}
				return
			}
			s.state.SetArmHistory(msgs)

			// Broadcast the latest assistant text to log subscribers.
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].OfAssistant != nil {
					text := msgs[i].OfAssistant.Content.OfString
					if text.Valid() && text.Value != "" {
						s.state.BroadcastArmLog("[agent] " + text.Value)
					}
					break
				}
			}

			// The turn ended with no pending tool call. If new voice messages
			// arrived in the meantime (and were not consumed mid-turn), drain
			// them into the context and keep the runtime alive for one more
			// turn; otherwise go idle.
			pending := s.state.DrainVoiceMessageQueue()
			if len(pending) == 0 {
				return
			}
			s.state.AppendArmHistory(openai.UserMessage(consumeResultString(pending)))
		}
	})
}

// runTurn drives one arm agent turn: repeatedly inference → parse compact
// <tool_call> blocks → execute tools → write results back. After EVERY tool
// result a role=user <queue_status> status bar is appended (design §4.3), so
// the model can notice new instructions mid-task and actively consume them.
func (s *ArmService) runTurn(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
	msgs := make([]openai.ChatCompletionMessageParamUnion, len(history))
	copy(msgs, history)

	toolCalls := 0
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		content, err := s.agent.ChatText(ctx, msgs)
		if err != nil {
			return nil, fmt.Errorf("arm llm chat: %w", err)
		}
		msgs = append(msgs, assistantTextMessage(content))

		calls := toolcalling.ParseCompactToolCalls(content)
		if len(calls) == 0 {
			return msgs, nil
		}

		for _, call := range calls {
			toolCalls++
			if toolCalls > armMaxToolCallsPerTurn {
				s.state.BroadcastArmLog("[error] arm agent exceeded max tool calls per turn")
				return msgs, nil
			}
			result := s.executeArmTool(ctx, call)
			// tool/user dual-role writeback (design §5), then the status bar.
			msgs = append(msgs,
				openai.ToolMessage(result, "arm-tool"),
				openai.UserMessage(result),
				openai.UserMessage(s.queueStatusMessage()),
			)
		}
	}
}

// queueStatusMessage renders the <queue_status> status bar from the current
// message_from_voice_agent_queue depth.
func (s *ArmService) queueStatusMessage() string {
	if s.state.VoiceMessageQueueLen() > 0 {
		return "<queue_status>not empty</queue_status>"
	}
	return "<queue_status>empty</queue_status>"
}

// consumeResultString renders the message produced by draining
// message_from_voice_agent_queue, per the get_message_from_voice_agent
// contract (api_of_embodied_tools.md §2.6).
func consumeResultString(msgs []string) string {
	if len(msgs) == 0 {
		return "当前没有新消息"
	}
	return "all_messages_from_voice_agent:" + strings.Join(msgs, ";")
}

// executeArmTool runs one compact tool call and returns the result string that
// is written back into the context. The four embodied tools go through the
// RESTful gateway; the two communication tools operate on the
// orchestration-held queues directly (design §3: the queues are held by the
// orchestration runtime).
func (s *ArmService) executeArmTool(ctx context.Context, call toolcalling.CompactToolCall) string {
	s.state.BroadcastArmLog(fmt.Sprintf("[tool] %s %s", call.Name, call.RawArgs))

	var result string
	var err error
	switch call.Name {
	case "get_current_coordinates":
		result, err = s.gateway.GetCurrentCoordinates(ctx)
	case "move_to_coordinates":
		args := toolcalling.SplitCompactArgs(call.RawArgs)
		if len(args) != 3 {
			err = fmt.Errorf("move_to_coordinates 需要 3 个参数 x,y,z，实际 %d 个", len(args))
		} else {
			result, err = s.gateway.MoveToCoordinates(ctx, args[0], args[1], args[2])
		}
	case "grab_the_block":
		color := strings.TrimSpace(call.RawArgs)
		if color == "" {
			err = fmt.Errorf("grab_the_block 缺少 color 参数")
		} else {
			result, err = s.gateway.GrabTheBlock(ctx, color)
		}
	case "release_the_block":
		result, err = s.gateway.ReleaseTheBlock(ctx)
	case "send_to_voice_agent":
		content := strings.TrimSpace(call.RawArgs)
		if content == "" {
			err = fmt.Errorf("send_to_voice_agent 缺少 content 参数")
		} else {
			s.state.SendToVoiceAgent(content)
			result = "发送成功"
		}
	case "get_message_from_voice_agent":
		result = consumeResultString(s.state.DrainVoiceMessageQueue())
	default:
		result = fmt.Sprintf("[NOT_FOUND] 未找到名为 '%s' 的工具", call.Name)
	}
	if err != nil {
		result = "[EXEC_ERROR] " + err.Error()
	}

	s.state.BroadcastArmLog(fmt.Sprintf("[tool_result] %s: %s", call.Name, result))
	return result
}

// assistantTextMessage wraps raw assistant text (which may contain compact
// <tool_call> blocks) into a history message, preserving it verbatim so the
// context matches the fine-tuning data format.
func assistantTextMessage(content string) openai.ChatCompletionMessageParamUnion {
	return openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			Content: openai.ChatCompletionAssistantMessageParamContentUnion{
				OfString: openai.String(content),
			},
		},
	}
}
