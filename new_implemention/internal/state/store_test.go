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
