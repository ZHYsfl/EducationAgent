package state

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
)

func TestUpdateRequirements(t *testing.T) {
	s := NewAppState()

	// Update partial requirements
	missing, err := s.UpdateRequirements(map[string]any{
		"topic": "math",
		"style": "simple",
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"total_pages", "audience"}, missing)

	// Update remaining requirements
	missing, err = s.UpdateRequirements(map[string]any{
		"total_pages": 15,
		"audience":    "middle school students",
	})
	assert.NoError(t, err)
	assert.Empty(t, missing)

	// After finalized, updates should fail
	s.MarkRequirementsFinalized()
	_, err = s.UpdateRequirements(map[string]any{"topic": "science"})
	assert.Error(t, err)
}

func TestRequireConfirm(t *testing.T) {
	s := NewAppState()

	// Incomplete requirements should fail
	err := s.RequireConfirm()
	assert.Error(t, err)

	// Complete requirements should succeed before finalized
	_, _ = s.UpdateRequirements(map[string]any{
		"topic":       "math",
		"style":       "simple",
		"total_pages": 10,
		"audience":    "kids",
	})
	err = s.RequireConfirm()
	assert.NoError(t, err)

	// After finalized, require_confirm should fail
	s.MarkRequirementsFinalized()
	err = s.RequireConfirm()
	assert.Error(t, err)
}

func TestMessageQueues(t *testing.T) {
	s := NewAppState()

	// PPT -> Voice queue
	s.SendToVoiceAgent("ppt says hello")
	msg, ok := s.FetchFromPPTMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "ppt says hello", msg)

	_, ok = s.FetchFromPPTMessageQueue()
	assert.False(t, ok)

	// Voice -> PPT queue
	s.SendToPPTAgent("voice says hi")
	msg, ok = s.FetchFromVoiceMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "voice says hi", msg)
}

func TestPPTHistory(t *testing.T) {
	s := NewAppState()
	s.AppendPPTHistory(openai.UserMessage("hello"))
	s.AppendPPTHistory(openai.UserMessage("world"))

	hist := s.GetPPTHistory()
	assert.Len(t, hist, 2)

	// Ensure copy is returned
	hist[0] = openai.UserMessage("modified")
	hist = s.GetPPTHistory()
	assert.Equal(t, "hello", hist[0].OfUser.Content.OfString.Value)
}

func TestConversationLifecycle(t *testing.T) {
	s := NewAppState()
	assert.False(t, s.IsConversationStarted())
	s.MarkConversationStarted()
	assert.True(t, s.IsConversationStarted())
}

func TestVADInterruptCache(t *testing.T) {
	s := NewAppState()
	_, ok := s.GetLastVADInterrupt()
	assert.False(t, ok)

	s.SetLastVADInterrupt(true)
	val, ok := s.GetLastVADInterrupt()
	assert.True(t, ok)
	assert.True(t, val)

	s.SetLastVADInterrupt(false)
	val, ok = s.GetLastVADInterrupt()
	assert.True(t, ok)
	assert.False(t, val)
}

func TestPeekPPTMessageQueue(t *testing.T) {
	s := NewAppState()
	_, ok := s.PeekPPTMessageQueue()
	assert.False(t, ok)

	s.SendToVoiceAgent("msg1")
	s.SendToVoiceAgent("msg2")

	msg, ok := s.PeekPPTMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "msg1", msg)

	// Peek should not remove the message.
	msg, ok = s.PeekPPTMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "msg1", msg)

	// Fetch should remove it.
	msg, ok = s.FetchFromPPTMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "msg1", msg)

	msg, ok = s.PeekPPTMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "msg2", msg)
}

func TestVoiceHistory(t *testing.T) {
	s := NewAppState()
	s.AppendVoiceHistory(openai.UserMessage("hello"))
	s.AppendVoiceHistory(openai.AssistantMessage("hi there"))

	hist := s.GetVoiceHistory()
	assert.Len(t, hist, 2)

	// Ensure copy is returned
	hist[0] = openai.UserMessage("modified")
	hist = s.GetVoiceHistory()
	assert.Equal(t, "hello", hist[0].OfUser.Content.OfString.Value)

	// SetVoiceHistory replaces the slice.
	s.SetVoiceHistory([]openai.ChatCompletionMessageParamUnion{openai.UserMessage("reset")})
	assert.Equal(t, 1, s.VoiceHistoryLen())
}
