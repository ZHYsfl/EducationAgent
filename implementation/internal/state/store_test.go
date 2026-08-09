package state

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
)

func TestMessageQueues(t *testing.T) {
	s := NewAppState()

	// arm → voice queue (message_from_arm_agent_queue)
	s.SendToVoiceAgent("arm says hello")
	msgs := s.DrainArmMessageQueue()
	assert.Equal(t, []string{"arm says hello"}, msgs)
	assert.Empty(t, s.DrainArmMessageQueue())

	// voice → arm queue (message_from_voice_agent_queue)
	s.SendToArmAgent("voice says hi")
	vmsgs := s.DrainVoiceMessageQueue()
	assert.Equal(t, []string{"voice says hi"}, vmsgs)
	assert.Equal(t, 0, s.VoiceMessageQueueLen())
}

func TestArmHistory(t *testing.T) {
	s := NewAppState()
	s.AppendArmHistory(openai.UserMessage("hello"))
	s.AppendArmHistory(openai.UserMessage("world"))

	hist := s.GetArmHistory()
	assert.Len(t, hist, 2)

	// Ensure copy is returned
	hist[0] = openai.UserMessage("modified")
	hist = s.GetArmHistory()
	assert.Equal(t, "hello", hist[0].OfUser.Content.OfString.Value)
}

func TestConversationLifecycle(t *testing.T) {
	s := NewAppState()
	assert.False(t, s.IsConversationStarted())
	s.ResetConversation()
	assert.True(t, s.IsConversationStarted())

	// Reset clears queues and histories.
	s.SendToArmAgent("task")
	s.SendToVoiceAgent("report")
	s.AppendArmHistory(openai.UserMessage("x"))
	s.AppendVoiceHistory(openai.UserMessage("y"))
	s.ResetConversation()
	assert.Equal(t, 0, s.VoiceMessageQueueLen())
	assert.Equal(t, 0, s.ArmMessageQueueLen())
	assert.Empty(t, s.GetArmHistory())
	assert.Empty(t, s.GetVoiceHistory())
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

func TestPeekArmMessageQueue(t *testing.T) {
	s := NewAppState()
	_, ok := s.PeekArmMessageQueue()
	assert.False(t, ok)

	s.SendToVoiceAgent("msg1")
	s.SendToVoiceAgent("msg2")

	msg, ok := s.PeekArmMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "msg1", msg)

	// Peek should not remove the message.
	msg, ok = s.PeekArmMessageQueue()
	assert.True(t, ok)
	assert.Equal(t, "msg1", msg)
	assert.Equal(t, 2, s.ArmMessageQueueLen())

	// Drain should remove all messages, oldest first.
	msgs := s.DrainArmMessageQueue()
	assert.Equal(t, []string{"msg1", "msg2"}, msgs)

	_, ok = s.PeekArmMessageQueue()
	assert.False(t, ok)
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
