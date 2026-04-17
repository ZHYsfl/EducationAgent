package state

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"educationagent/internal/model"

	"github.com/openai/openai-go/v3"
)

// AppState holds all mutable application state protected by a mutex.
type AppState struct {
	mu                    sync.RWMutex
	req                   model.Requirements
	requirementsFinalized bool
	pptMessageQueue       []string
	voiceMessageQueue     []string
	pptHistory            []openai.ChatCompletionMessageParamUnion

	// Voice turn state
	conversationStarted bool
	lastVADInterrupt    *bool
	voiceHistory        []openai.ChatCompletionMessageParamUnion
	voiceTurnMu         sync.Mutex
}

// NewAppState creates a fresh application state.
func NewAppState() *AppState {
	return &AppState{}
}

// UpdateRequirements merges the provided fields into the existing requirements.
// It returns the list of missing fields. If requirements are already finalized,
// it returns an error because the update_requirements tool has disappeared.
func (s *AppState) UpdateRequirements(req map[string]any) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.requirementsFinalized {
		return nil, errors.New("update_requirements tool has disappeared")
	}

	if v, ok := req["topic"]; ok {
		if str, ok := v.(string); ok {
			s.req.Topic = &str
		}
	}
	if v, ok := req["style"]; ok {
		if str, ok := v.(string); ok {
			s.req.Style = &str
		}
	}
	if v, ok := req["total_pages"]; ok {
		switch n := v.(type) {
		case int:
			s.req.TotalPages = &n
		case int64:
			i := int(n)
			s.req.TotalPages = &i
		case float64:
			i := int(n)
			s.req.TotalPages = &i
		case float32:
			i := int(n)
			s.req.TotalPages = &i
		default:
			// ignore unrecognised type
		}
	}
	if v, ok := req["audience"]; ok {
		if str, ok := v.(string); ok {
			s.req.Audience = &str
		}
	}

	missing := s.req.MissingFields()
	return missing, nil
}

// GetRequirements returns a snapshot of current requirements.
func (s *AppState) GetRequirements() model.Requirements {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.req
}

// RequireConfirm verifies that all requirement fields are present.
// If requirements are finalized it returns an error.
func (s *AppState) RequireConfirm() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.requirementsFinalized {
		return errors.New("require_confirm tool has disappeared")
	}
	if !s.req.IsComplete() {
		return fmt.Errorf("requirements incomplete, missing: %v", s.req.MissingFields())
	}
	return nil
}

// MarkRequirementsFinalized locks the requirements phase forever.
func (s *AppState) MarkRequirementsFinalized() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requirementsFinalized = true
}

// IsRequirementsFinalized reports whether the requirements phase is locked.
func (s *AppState) IsRequirementsFinalized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.requirementsFinalized
}

// SendToPPTAgent enqueues a message from the voice agent to the ppt agent
// (stored in the voice message queue).
func (s *AppState) SendToPPTAgent(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceMessageQueue = append(s.voiceMessageQueue, data)
}

// FetchFromVoiceMessageQueue dequeues the oldest message from the voice message queue.
// It returns the message and true if one existed.
func (s *AppState) FetchFromVoiceMessageQueue() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.voiceMessageQueue) == 0 {
		return "", false
	}
	msg := s.voiceMessageQueue[0]
	s.voiceMessageQueue = s.voiceMessageQueue[1:]
	return msg, true
}

// PeekVoiceMessageQueue returns the oldest voice message without removing it.
func (s *AppState) PeekVoiceMessageQueue() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.voiceMessageQueue) == 0 {
		return "", false
	}
	return s.voiceMessageQueue[0], true
}

// DrainVoiceMessageQueue removes and returns all messages from the voice message queue.
func (s *AppState) DrainVoiceMessageQueue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.voiceMessageQueue))
	copy(out, s.voiceMessageQueue)
	s.voiceMessageQueue = s.voiceMessageQueue[:0]
	return out
}

// VoiceMessageQueueLen returns the number of pending messages in the voice message queue.
func (s *AppState) VoiceMessageQueueLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.voiceMessageQueue)
}

// SendToVoiceAgent enqueues a message from the ppt agent to the voice agent
// (stored in the ppt message queue).
func (s *AppState) SendToVoiceAgent(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pptMessageQueue = append(s.pptMessageQueue, data)
}

// FetchFromPPTMessageQueue drains all messages from the ppt message queue and
// returns them concatenated with " | ", or an empty string if the queue is empty.
func (s *AppState) FetchFromPPTMessageQueue() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pptMessageQueue) == 0 {
		return "", nil
	}
	msgs := make([]string, len(s.pptMessageQueue))
	copy(msgs, s.pptMessageQueue)
	s.pptMessageQueue = s.pptMessageQueue[:0]
	return strings.Join(msgs, " | "), nil
}

// PeekPPTMessageQueue returns the oldest ppt message without removing it.
func (s *AppState) PeekPPTMessageQueue() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.pptMessageQueue) == 0 {
		return "", false
	}
	return s.pptMessageQueue[0], true
}

// AppendPPTHistory appends a message to the PPT agent's conversation history.
func (s *AppState) AppendPPTHistory(msg openai.ChatCompletionMessageParamUnion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pptHistory = append(s.pptHistory, msg)
}

// GetPPTHistory returns a copy of the PPT agent's conversation history.
func (s *AppState) GetPPTHistory() []openai.ChatCompletionMessageParamUnion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]openai.ChatCompletionMessageParamUnion, len(s.pptHistory))
	copy(out, s.pptHistory)
	return out
}

// SetPPTHistory replaces the entire PPT agent history.
func (s *AppState) SetPPTHistory(history []openai.ChatCompletionMessageParamUnion) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pptHistory = make([]openai.ChatCompletionMessageParamUnion, len(history))
	copy(s.pptHistory, history)
}

// ---------------------------------------------------------------------------
// Conversation lifecycle
// ---------------------------------------------------------------------------

// MarkConversationStarted records that the conversation has begun.
func (s *AppState) MarkConversationStarted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conversationStarted = true
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

// LockVoiceTurn blocks until the current voice turn (including its action sequence) finishes.
func (s *AppState) LockVoiceTurn() {
	s.voiceTurnMu.Lock()
}

// UnlockVoiceTurn releases the voice turn lock.
func (s *AppState) UnlockVoiceTurn() {
	s.voiceTurnMu.Unlock()
}
