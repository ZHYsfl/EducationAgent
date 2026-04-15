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

func TestPPTServiceOnVoiceMessageFeedbackRestartsRuntime(t *testing.T) {
	st := state.NewAppState()
	_, _ = st.UpdateRequirements(map[string]any{
		"topic":       "math",
		"style":       "simple",
		"total_pages": 10,
		"audience":    "kids",
	})

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	ps := NewPPTService(st, agent, NewKBService(), NewSearchService())

	// First message starts runtime
	ps.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		<-ctx.Done()
		return history, ctx.Err()
	})
	_ = ps.OnVoiceMessage("initial requirements")
	require.True(t, ps.IsRuntimeRunning())

	// Wait a bit then send feedback
	time.Sleep(50 * time.Millisecond)
	var runCount atomic.Int32
	ps.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		runCount.Add(1)
		<-ctx.Done()
		return history, ctx.Err()
	})

	err := ps.OnVoiceMessage("feedback: make font bigger")
	require.NoError(t, err)

	assert.Eventually(t, func() bool { return runCount.Load() >= 1 }, time.Second, 10*time.Millisecond)
	assert.True(t, ps.IsRuntimeRunning())

	ps.StopRuntime()
	ps.WaitRuntime()
}

func TestPPTServiceSendToVoiceAgentStopsRuntime(t *testing.T) {
	st := state.NewAppState()
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	ps := NewPPTService(st, agent, NewKBService(), NewSearchService())

	ps.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		<-ctx.Done()
		return history, ctx.Err()
	})

	st.MarkRequirementsFinalized()
	st.SetPPTHistory([]openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("test"),
	})
	ps.startRuntime(model.Requirements{Topic: strPtr("x"), Style: strPtr("y"), TotalPages: intPtr(1), Audience: strPtr("z")})
	require.Eventually(t, ps.IsRuntimeRunning, time.Second, 10*time.Millisecond)

	err := ps.SendToVoiceAgent("ppt done")
	require.NoError(t, err)

	ps.WaitRuntime()
	assert.False(t, ps.IsRuntimeRunning())

	msg, ok := st.FetchFromPPTMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "ppt done", msg)
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
