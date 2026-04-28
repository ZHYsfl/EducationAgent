package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"educationagent/internal/model"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
)

func TestVoiceServiceUpdateRequirements(t *testing.T) {
	st := state.NewAppState()
	vs := NewVoiceService(st)

	missing, err := vs.UpdateRequirements(map[string]any{
		"topic": "math",
		"style": "simple",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"total_pages", "audience"}, missing)
}

func TestPPTServiceOnVoiceMessageFirstTimeStartsRuntime(t *testing.T) {
	st := state.NewAppState()
	_, _ = st.UpdateRequirements(map[string]any{
		"topic":       "math",
		"style":       "simple",
		"total_pages": 10,
		"audience":    "kids",
	})

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	ps := NewPPTService(st, agent, NewKBService(), NewSearchService())

	var called atomic.Bool
	ps.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		called.Store(true)
		<-ctx.Done()
		return history, ctx.Err()
	})

	err := ps.OnVoiceMessage("initial requirements")
	require.NoError(t, err)

	assert.True(t, st.IsRequirementsFinalized())
	assert.Eventually(t, func() bool { return called.Load() }, time.Second, 10*time.Millisecond)
	assert.True(t, ps.IsRuntimeRunning())

	ps.StopRuntime()
	ps.WaitRuntime()
}

func TestPPTServiceOnVoiceMessageDoesNotCancelRunningRuntime(t *testing.T) {
	st := state.NewAppState()
	_, _ = st.UpdateRequirements(map[string]any{
		"topic":       "math",
		"style":       "simple",
		"total_pages": 10,
		"audience":    "kids",
	})

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	ps := NewPPTService(st, agent, NewKBService(), NewSearchService())

	blocker := make(chan struct{})
	ps.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		<-blocker
		return history, nil
	})

	// First message starts runtime.
	_ = ps.OnVoiceMessage("initial requirements")
	require.Eventually(t, func() bool { return ps.IsRuntimeRunning() }, time.Second, 10*time.Millisecond)

	// Feedback arrives while runtime is running.
	_ = ps.OnVoiceMessage("feedback: make font bigger")

	// Runtime should NOT have been cancelled; it stays alive.
	time.Sleep(50 * time.Millisecond)
	assert.True(t, ps.IsRuntimeRunning())

	close(blocker)
	ps.StopRuntime()
	ps.WaitRuntime()
}

func TestPPTServiceOnVoiceMessageRestartsIdleRuntime(t *testing.T) {
	st := state.NewAppState()
	_, _ = st.UpdateRequirements(map[string]any{
		"topic":       "math",
		"style":       "simple",
		"total_pages": 10,
		"audience":    "kids",
	})

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	ps := NewPPTService(st, agent, NewKBService(), NewSearchService())

	// First message: start runtime, observe it is running, then let it finish.
	firstBlocker := make(chan struct{})
	ps.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		<-firstBlocker
		return history, nil
	})
	_ = ps.OnVoiceMessage("initial requirements")
	require.Eventually(t, func() bool { return ps.IsRuntimeRunning() }, time.Second, 10*time.Millisecond)
	close(firstBlocker)
	ps.WaitRuntime()
	assert.False(t, ps.IsRuntimeRunning())

	// Second message: runtime is idle, feedback should trigger a restart.
	var called atomic.Bool
	blocker := make(chan struct{})
	ps.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		called.Store(true)
		<-blocker
		return history, nil
	})

	_ = ps.OnVoiceMessage("feedback: make font bigger")
	require.Eventually(t, func() bool { return called.Load() }, time.Second, 10*time.Millisecond)
	assert.True(t, ps.IsRuntimeRunning())

	close(blocker)
	ps.WaitRuntime()
}

func TestPPTServiceSendToVoiceAgentDoesNotStopRuntime(t *testing.T) {
	st := state.NewAppState()
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	ps := NewPPTService(st, agent, NewKBService(), NewSearchService())

	blocker := make(chan struct{})
	ps.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		<-blocker
		return history, nil
	})

	st.MarkRequirementsFinalized()
	st.SetPPTHistory([]openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("test"),
	})
	ps.startRuntime(model.Requirements{Topic: strPtr("x"), Style: strPtr("y"), TotalPages: intPtr(1), Audience: strPtr("z")}, "")
	require.Eventually(t, ps.IsRuntimeRunning, time.Second, 10*time.Millisecond)

	err := ps.SendToVoiceAgent("ppt done")
	require.NoError(t, err)

	// Runtime should still be running (SendToVoiceAgent no longer stops it).
	time.Sleep(50 * time.Millisecond)
	assert.True(t, ps.IsRuntimeRunning())

	close(blocker)
	ps.WaitRuntime()
	assert.False(t, ps.IsRuntimeRunning())

	msg, err := st.FetchFromPPTMessageQueue()
	assert.NoError(t, err)
	assert.Equal(t, "ppt done", msg)
}

func TestPPTServiceFetchFromVoiceMessageQueueTool(t *testing.T) {
	st := state.NewAppState()
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	ps := NewPPTService(st, agent, NewKBService(), NewSearchService())

	st.SendToPPTAgent("hello")
	st.SendToPPTAgent("world")

	res, err := ps.fetchFromVoiceMessageQueueTool(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "hello | world", res)

	res, err = ps.fetchFromVoiceMessageQueueTool(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "queue is empty", res)
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
