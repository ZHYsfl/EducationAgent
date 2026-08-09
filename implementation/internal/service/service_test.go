package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
	"educationagent/internal/tools"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestArmService(st *state.AppState) *ArmService {
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	return NewArmService(st, agent, tools.NewArmGateway("http://127.0.0.1:1"))
}

func TestArmServiceOnVoiceMessageStartsRuntime(t *testing.T) {
	st := state.NewAppState()
	svc := newTestArmService(st)

	var called atomic.Bool
	svc.SetRunTurnFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		called.Store(true)
		<-ctx.Done()
		return history, ctx.Err()
	})

	err := svc.OnVoiceMessage("抓取 red 物块并放到 (1.0,2.0,3.0)。")
	require.NoError(t, err)

	assert.Eventually(t, func() bool { return called.Load() }, time.Second, 10*time.Millisecond)
	assert.True(t, svc.IsRuntimeRunning())

	// The task entered the context as one consumed user message.
	history := st.GetArmHistory()
	require.GreaterOrEqual(t, len(history), 2)
	assert.NotNil(t, history[0].OfSystem)
	last := history[len(history)-1]
	require.NotNil(t, last.OfUser)
	assert.Equal(t, "all_messages_from_voice_agent:抓取 red 物块并放到 (1.0,2.0,3.0)。", last.OfUser.Content.OfString.Value)

	svc.StopRuntime()
	svc.WaitRuntime()
}

func TestArmServiceOnVoiceMessageDoesNotCancelRunningRuntime(t *testing.T) {
	st := state.NewAppState()
	svc := newTestArmService(st)

	blocker := make(chan struct{})
	svc.SetRunTurnFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		<-blocker
		return history, nil
	})

	// First message starts the runtime.
	_ = svc.OnVoiceMessage("initial task")
	require.Eventually(t, func() bool { return svc.IsRuntimeRunning() }, time.Second, 10*time.Millisecond)

	// A change message arrives while the runtime is running.
	_ = svc.OnVoiceMessage("用户改主意了，请改抓 yellow 物块")

	// Runtime should NOT have been cancelled; the message waits in the queue.
	time.Sleep(50 * time.Millisecond)
	assert.True(t, svc.IsRuntimeRunning())
	assert.Equal(t, 1, st.VoiceMessageQueueLen())

	close(blocker)
	svc.StopRuntime()
	svc.WaitRuntime()
}

func TestArmServiceOnVoiceMessageRestartsIdleRuntime(t *testing.T) {
	st := state.NewAppState()
	svc := newTestArmService(st)

	// First message: start runtime, observe it running, then let it finish.
	firstBlocker := make(chan struct{})
	svc.SetRunTurnFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		<-firstBlocker
		return history, nil
	})
	_ = svc.OnVoiceMessage("initial task")
	require.Eventually(t, func() bool { return svc.IsRuntimeRunning() }, time.Second, 10*time.Millisecond)
	close(firstBlocker)
	svc.WaitRuntime()
	assert.False(t, svc.IsRuntimeRunning())

	// Second message: runtime is idle, so it should restart (idle auto-consume).
	var called atomic.Bool
	blocker := make(chan struct{})
	svc.SetRunTurnFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		called.Store(true)
		<-blocker
		return history, nil
	})

	_ = svc.OnVoiceMessage("new task")
	require.Eventually(t, func() bool { return called.Load() }, time.Second, 10*time.Millisecond)
	assert.True(t, svc.IsRuntimeRunning())

	close(blocker)
	svc.StopRuntime()
	svc.WaitRuntime()
}

func TestArmServiceSendToVoiceAgentDoesNotStopRuntime(t *testing.T) {
	st := state.NewAppState()
	svc := newTestArmService(st)

	blocker := make(chan struct{})
	svc.SetRunTurnFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		<-blocker
		return history, nil
	})

	_ = svc.OnVoiceMessage("initial task")
	require.Eventually(t, svc.IsRuntimeRunning, time.Second, 10*time.Millisecond)

	err := svc.SendToVoiceAgent("任务完成")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	assert.True(t, svc.IsRuntimeRunning())

	close(blocker)
	svc.StopRuntime()
	svc.WaitRuntime()

	msgs := st.DrainArmMessageQueue()
	assert.Equal(t, []string{"任务完成"}, msgs)
}

// gatewayStub serves the embodied-tool RESTful gateway contract
// (api_of_embodied_tools.md): {code, result, error} envelope.
func gatewayStub(t *testing.T, results map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, ok := results[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "result": result, "error": nil})
	}))
}

func TestExecuteArmToolEmbodiedContracts(t *testing.T) {
	st := state.NewAppState()
	gw := gatewayStub(t, map[string]string{
		"/api/v1/get_current_coordinates": "我的坐标是0.0,0.0,0.0",
		"/api/v1/move_to_coordinates":     "成功到达1.0,2.0,3.0",
		"/api/v1/grab_the_block":          "有这种颜色的物块，且夹取物块成功",
		"/api/v1/release_the_block":       "成功释放物块",
	})
	defer gw.Close()

	svc := NewArmService(st, toolcalling.NewAgent(toolcalling.LLMConfig{}), tools.NewArmGateway(gw.URL))
	ctx := context.Background()

	assert.Equal(t, "我的坐标是0.0,0.0,0.0",
		svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "get_current_coordinates"}))
	assert.Equal(t, "成功到达1.0,2.0,3.0",
		svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "move_to_coordinates", RawArgs: "1.0,2.0,3.0"}))
	assert.Equal(t, "有这种颜色的物块，且夹取物块成功",
		svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "grab_the_block", RawArgs: "red"}))
	assert.Equal(t, "成功释放物块",
		svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "release_the_block"}))

	// move_to_coordinates with wrong arity must surface an error, not call the gateway.
	res := svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "move_to_coordinates", RawArgs: "1.0,2.0"})
	assert.True(t, strings.HasPrefix(res, "[EXEC_ERROR]"), res)
}

func TestExecuteArmToolCommunicationContracts(t *testing.T) {
	st := state.NewAppState()
	svc := newTestArmService(st)
	ctx := context.Background()

	// Empty queue → fixed contract string.
	assert.Equal(t, "当前没有新消息",
		svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "get_message_from_voice_agent"}))

	// Non-empty queue → drained, joined with ";".
	st.SendToArmAgent("用户改主意了，请改抓 yellow 物块")
	st.SendToArmAgent("目标位置不变")
	assert.Equal(t, "all_messages_from_voice_agent:用户改主意了，请改抓 yellow 物块;目标位置不变",
		svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "get_message_from_voice_agent"}))
	assert.Equal(t, 0, st.VoiceMessageQueueLen())

	// send_to_voice_agent enqueues into message_from_arm_agent_queue.
	assert.Equal(t, "发送成功",
		svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "send_to_voice_agent", RawArgs: "已到达目标位置，任务完成。"}))
	msgs := st.DrainArmMessageQueue()
	assert.Equal(t, []string{"已到达目标位置，任务完成。"}, msgs)

	// Unknown tool → structured error marker.
	res := svc.executeArmTool(ctx, toolcalling.CompactToolCall{Name: "fly"})
	assert.True(t, strings.HasPrefix(res, "[NOT_FOUND]"), res)
}

// llmStub serves an OpenAI-compatible /chat/completions endpoint returning the
// queued assistant contents in order.
func llmStub(t *testing.T, contents ...string) *httptest.Server {
	t.Helper()
	var idx atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(idx.Add(1)) - 1
		content := ""
		if i < len(contents) {
			content = contents[i]
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1,
			"model":   "arm-agent",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
		})
	}))
}

// TestRunTurnQueueStatusInjection verifies the arm-side core mechanism
// (design §4.3): after EVERY tool result, a role=user <queue_status> status
// bar is appended, and tool results are written back with tool/user dual role.
func TestRunTurnQueueStatusInjection(t *testing.T) {
	st := state.NewAppState()
	gw := gatewayStub(t, map[string]string{
		"/api/v1/get_current_coordinates": "我的坐标是0.0,0.0,0.0",
	})
	defer gw.Close()
	llm := llmStub(t,
		"我先确认当前坐标。\n<tool_call>\nget_current_coordinates:\n</tool_call>",
		"已确认坐标，开始执行。",
	)
	defer llm.Close()

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{BaseURL: llm.URL, Model: "arm-agent", APIKey: "dummy"})
	svc := NewArmService(st, agent, tools.NewArmGateway(gw.URL))

	history := []openai.ChatCompletionMessageParamUnion{
		svc.buildSystemMessage(),
		openai.UserMessage("all_messages_from_voice_agent:抓取 red 物块并放到 (1.0,2.0,3.0)。"),
	}
	msgs, err := svc.runTurn(context.Background(), history)
	require.NoError(t, err)

	// Expected tail: assistant(tool_call) → tool result → user result (dual
	// role) → user <queue_status> → assistant(final text).
	require.GreaterOrEqual(t, len(msgs), len(history)+5)
	tail := msgs[len(history):]

	require.NotNil(t, tail[0].OfAssistant)
	assert.Contains(t, tail[0].OfAssistant.Content.OfString.Value, "<tool_call>")

	require.NotNil(t, tail[1].OfTool)
	assert.Equal(t, "我的坐标是0.0,0.0,0.0", tail[1].OfTool.Content.OfString.Value)

	require.NotNil(t, tail[2].OfUser)
	assert.Equal(t, "我的坐标是0.0,0.0,0.0", tail[2].OfUser.Content.OfString.Value)

	require.NotNil(t, tail[3].OfUser)
	assert.Equal(t, "<queue_status>empty</queue_status>", tail[3].OfUser.Content.OfString.Value)

	require.NotNil(t, tail[4].OfAssistant)
	assert.Equal(t, "已确认坐标，开始执行。", tail[4].OfAssistant.Content.OfString.Value)
}

// TestRunTurnQueueStatusNotEmpty verifies that the status bar reports
// "not empty" while a new instruction is pending, so the model can actively
// consume it mid-task.
func TestRunTurnQueueStatusNotEmpty(t *testing.T) {
	st := state.NewAppState()
	gw := gatewayStub(t, map[string]string{
		"/api/v1/get_current_coordinates": "我的坐标是0.0,0.0,0.0",
	})
	defer gw.Close()
	llm := llmStub(t,
		"<tool_call>\nget_current_coordinates:\n</tool_call>",
		"队列里有新消息，我先消费看看。\n<tool_call>\nget_message_from_voice_agent:\n</tool_call>",
		"收到变更，改抓 yellow 物块。",
	)
	defer llm.Close()

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{BaseURL: llm.URL, Model: "arm-agent", APIKey: "dummy"})
	svc := NewArmService(st, agent, tools.NewArmGateway(gw.URL))

	// A change instruction arrives mid-task.
	st.SendToArmAgent("用户改主意了，请改抓 yellow 物块")

	msgs, err := svc.runTurn(context.Background(), []openai.ChatCompletionMessageParamUnion{
		svc.buildSystemMessage(),
		openai.UserMessage("all_messages_from_voice_agent:抓取 red 物块并放到 (1.0,2.0,3.0)。"),
	})
	require.NoError(t, err)

	var sawNotEmpty, sawConsumed bool
	for _, m := range msgs {
		if m.OfUser == nil {
			continue
		}
		text := m.OfUser.Content.OfString.Value
		if text == "<queue_status>not empty</queue_status>" {
			sawNotEmpty = true
		}
		if text == "all_messages_from_voice_agent:用户改主意了，请改抓 yellow 物块" {
			sawConsumed = true
		}
	}
	assert.True(t, sawNotEmpty, "expected a not-empty status bar while the queue had a pending message")
	assert.True(t, sawConsumed, "expected the consumed message to enter the context")
	assert.Equal(t, 0, st.VoiceMessageQueueLen())
}

func TestParseCompactToolCalls(t *testing.T) {
	calls := toolcalling.ParseCompactToolCalls(
		"收到。\n<tool_call>\nmove_to_coordinates:0.5,0.2,0.1\n</tool_call>")
	require.Len(t, calls, 1)
	assert.Equal(t, "move_to_coordinates", calls[0].Name)
	assert.Equal(t, "0.5,0.2,0.1", calls[0].RawArgs)
	assert.Equal(t, []string{"0.5", "0.2", "0.1"}, toolcalling.SplitCompactArgs(calls[0].RawArgs))

	// Free-form content keeps commas/colons intact.
	calls = toolcalling.ParseCompactToolCalls(
		"<tool_call>\nsend_to_voice_agent:已到达 (1.0,2.0,3.0)，任务完成。\n</tool_call>")
	require.Len(t, calls, 1)
	assert.Equal(t, "send_to_voice_agent", calls[0].Name)
	assert.Equal(t, "已到达 (1.0,2.0,3.0)，任务完成。", calls[0].RawArgs)

	assert.Empty(t, toolcalling.ParseCompactToolCalls("纯文本，没有工具调用"))
}

// TestSplitCompressionWindowBelowThreshold verifies that compression does not
// trigger while the history fits within voiceCompressThreshold.
func TestSplitCompressionWindowBelowThreshold(t *testing.T) {
	history := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("帮我抓红色的物块"),
		openai.UserMessage("<queue_status>empty</queue_status>"),
		openai.AssistantMessage("好的。"),
	}
	old, recent := splitCompressionWindow(history)
	assert.Nil(t, old)
	assert.Equal(t, len(history), len(recent))
}

// TestSplitCompressionWindowKeepsPairs verifies that the compression cut never
// leaves a dangling <queue_status> status bar or tool result at the head of
// the recent window — stale snapshots are moved into the compressed chunk.
func TestSplitCompressionWindowKeepsPairs(t *testing.T) {
	history := make([]openai.ChatCompletionMessageParamUnion, 0, voiceCompressThreshold+4)
	for i := 0; i < voiceCompressThreshold+4; i++ {
		history = append(history, openai.UserMessage(fmt.Sprintf("第 %d 轮", i)))
	}
	// Force the message at the nominal cut point to be a status bar, and the
	// one after it a tool result: both must be absorbed into the old chunk.
	cut := len(history) - voiceCompressKeepRecent
	history[cut] = openai.UserMessage("<queue_status>empty</queue_status>")
	history[cut+1] = openai.ToolMessage("发送成功", "voice-agent-tool")

	old, recent := splitCompressionWindow(history)
	require.NotNil(t, old)
	assert.Equal(t, cut+2, len(old))
	assert.Equal(t, len(history)-cut-2, len(recent))

	head := recent[0]
	require.NotNil(t, head.OfUser)
	assert.NotContains(t, head.OfUser.Content.OfString.Value, "<queue_status>")
}
