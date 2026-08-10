package state

import (
	"sync"

	"github.com/openai/openai-go/v3"
)

// AppState holds all mutable application state protected by a mutex.
//
// The two FIFO queues are the system-level shared state of the async
// dual-agent design (see async_dual_agent_system_design.md §4.1):
//   - voiceMessageQueue: voice → arm direction (message_from_voice_agent_queue)
//   - armMessageQueue:   arm → voice direction (message_from_arm_agent_queue)
type AppState struct {
	mu                sync.RWMutex
	armMessageQueue   []string
	voiceMessageQueue []string
	armHistory        []openai.ChatCompletionMessageParamUnion

	// Arm agent activity log broadcast
	armLogMu     sync.Mutex
	armLogSubs   []chan string
	armLogRecent []string // ring buffer for late SSE subscribers

	// Voice turn state
	conversationStarted bool
	lastVADInterrupt    *bool
	voiceHistory        []openai.ChatCompletionMessageParamUnion
	voiceSummary        string // rolling summary of compressed older voice turns
	voiceTurnMu         sync.Mutex
}

// NewAppState creates a fresh application state.
func NewAppState() *AppState {
	return &AppState{}
}

// ---------------------------------------------------------------------------
// message_from_voice_agent_queue (voice → arm)
// ---------------------------------------------------------------------------

// SendToArmAgent enqueues a message from the voice agent to the arm agent.
func (s *AppState) SendToArmAgent(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceMessageQueue = append(s.voiceMessageQueue, data)
}

// DrainVoiceMessageQueue removes and returns all messages from the
// voice → arm queue (oldest first).
func (s *AppState) DrainVoiceMessageQueue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.voiceMessageQueue))
	copy(out, s.voiceMessageQueue)
	s.voiceMessageQueue = s.voiceMessageQueue[:0]
	return out
}

// VoiceMessageQueueLen returns the number of pending messages in the
// voice → arm queue.
func (s *AppState) VoiceMessageQueueLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.voiceMessageQueue)
}

// ---------------------------------------------------------------------------
// message_from_arm_agent_queue (arm → voice)
// ---------------------------------------------------------------------------

// SendToVoiceAgent enqueues a message from the arm agent to the voice agent.
func (s *AppState) SendToVoiceAgent(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armMessageQueue = append(s.armMessageQueue, data)
}

// DrainArmMessageQueue removes and returns all messages from the
// arm → voice queue (oldest first).
func (s *AppState) DrainArmMessageQueue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.armMessageQueue))
	copy(out, s.armMessageQueue)
	s.armMessageQueue = s.armMessageQueue[:0]
	return out
}

// PeekArmMessageQueue returns the oldest arm → voice message without removing it.
func (s *AppState) PeekArmMessageQueue() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.armMessageQueue) == 0 {
		return "", false
	}
	return s.armMessageQueue[0], true
}

// ArmMessageQueueLen returns the number of pending messages in the
// arm → voice queue.
func (s *AppState) ArmMessageQueueLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.armMessageQueue)
}

// ---------------------------------------------------------------------------
// Arm agent conversation history
// ---------------------------------------------------------------------------

// AppendArmHistory appends a message to the arm agent's conversation history.
func (s *AppState) AppendArmHistory(msg openai.ChatCompletionMessageParamUnion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armHistory = append(s.armHistory, msg)
}

// GetArmHistory returns a copy of the arm agent's conversation history.
func (s *AppState) GetArmHistory() []openai.ChatCompletionMessageParamUnion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]openai.ChatCompletionMessageParamUnion, len(s.armHistory))
	copy(out, s.armHistory)
	return out
}

// SetArmHistory replaces the entire arm agent history.
func (s *AppState) SetArmHistory(history []openai.ChatCompletionMessageParamUnion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armHistory = make([]openai.ChatCompletionMessageParamUnion, len(history))
	copy(s.armHistory, history)
}

// ---------------------------------------------------------------------------
// Conversation lifecycle
// ---------------------------------------------------------------------------

// ResetConversation clears all state for a fresh conversation.
func (s *AppState) ResetConversation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceHistory = nil
	s.voiceSummary = ""
	s.lastVADInterrupt = nil
	s.conversationStarted = true
	s.armHistory = nil
	s.armMessageQueue = s.armMessageQueue[:0]
	s.voiceMessageQueue = s.voiceMessageQueue[:0]
}

// IsConversationStarted reports whether the conversation has begun.
func (s *AppState) IsConversationStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.conversationStarted
}

// ---------------------------------------------------------------------------
// VAD interrupt cache
// ---------------------------------------------------------------------------

// SetLastVADInterrupt stores the result of the most recent vad_start check.
func (s *AppState) SetLastVADInterrupt(val bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastVADInterrupt = &val
}

// GetLastVADInterrupt returns the cached vad_start result and true if it exists.
func (s *AppState) GetLastVADInterrupt() (bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastVADInterrupt == nil {
		return false, false
	}
	return *s.lastVADInterrupt, true
}

// ClearLastVADInterrupt discards the cached vad_start result so a stale
// decision is never reused by a later vad_end.
func (s *AppState) ClearLastVADInterrupt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastVADInterrupt = nil
}

// ---------------------------------------------------------------------------
// Voice agent conversation history
// ---------------------------------------------------------------------------

// AppendVoiceHistory appends a message to the voice agent's history.
func (s *AppState) AppendVoiceHistory(msg openai.ChatCompletionMessageParamUnion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceHistory = append(s.voiceHistory, msg)
}

// GetVoiceHistory returns a copy of the voice agent's history.
func (s *AppState) GetVoiceHistory() []openai.ChatCompletionMessageParamUnion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]openai.ChatCompletionMessageParamUnion, len(s.voiceHistory))
	copy(out, s.voiceHistory)
	return out
}

// SetVoiceHistory replaces the entire voice agent history.
func (s *AppState) SetVoiceHistory(history []openai.ChatCompletionMessageParamUnion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceHistory = make([]openai.ChatCompletionMessageParamUnion, len(history))
	copy(s.voiceHistory, history)
}

// VoiceHistoryLen returns the length of the voice agent history.
func (s *AppState) VoiceHistoryLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.voiceHistory)
}

// GetVoiceSummary returns the rolling summary of compressed older voice turns.
func (s *AppState) GetVoiceSummary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.voiceSummary
}

// SetVoiceSummary replaces the rolling voice summary.
func (s *AppState) SetVoiceSummary(summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceSummary = summary
}

// LockVoiceTurn blocks until the current voice turn (including its tool-call
// sequence) finishes.
func (s *AppState) LockVoiceTurn() {
	s.voiceTurnMu.Lock()
}

// UnlockVoiceTurn releases the voice turn lock.
func (s *AppState) UnlockVoiceTurn() {
	s.voiceTurnMu.Unlock()
}

// ---------------------------------------------------------------------------
// Arm log broadcast (fan-out to SSE subscribers)
// ---------------------------------------------------------------------------

const armLogRecentMax = 500

// SubscribeArmLog registers a subscriber and returns buffered lines already broadcast (for SSE replay).
func (s *AppState) SubscribeArmLog() (ch chan string, replay []string) {
	ch = make(chan string, 256)
	s.armLogMu.Lock()
	replay = append([]string(nil), s.armLogRecent...)
	s.armLogSubs = append(s.armLogSubs, ch)
	s.armLogMu.Unlock()
	return ch, replay
}

func (s *AppState) UnsubscribeArmLog(ch chan string) {
	s.armLogMu.Lock()
	defer s.armLogMu.Unlock()
	subs := s.armLogSubs[:0]
	for _, sub := range s.armLogSubs {
		if sub != ch {
			subs = append(subs, sub)
		}
	}
	s.armLogSubs = subs
}

func (s *AppState) BroadcastArmLog(msg string) {
	s.armLogMu.Lock()
	s.armLogRecent = append(s.armLogRecent, msg)
	if len(s.armLogRecent) > armLogRecentMax {
		s.armLogRecent = s.armLogRecent[len(s.armLogRecent)-armLogRecentMax:]
	}
	subs := append([]chan string(nil), s.armLogSubs...)
	s.armLogMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- msg:
		default:
		}
	}
}
