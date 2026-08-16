package state

import (
	"errors"
	"fmt"
	"sync"

	"educationagent/internal/model"
)

// AppState holds all mutable application state protected by a mutex.
type AppState struct {
	mu                    sync.RWMutex
	req                   model.Requirements
	requirementsFinalized bool
	pptToVoiceQueue       []string
	voiceToPPTQueue       []string
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
		if str, ok := v.(string); ok && str != "" {
			s.req.Topic = &str
		}
	}
	if v, ok := req["style"]; ok {
		if str, ok := v.(string); ok && str != "" {
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
		}
	}
	if v, ok := req["audience"]; ok {
		if str, ok := v.(string); ok && str != "" {
			s.req.Audience = &str
		}
	}

	return s.req.MissingFields(), nil
}

// GetRequirements returns a snapshot of current requirements.
func (s *AppState) GetRequirements() model.Requirements {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.req
}

// RequireConfirm verifies that all requirement fields are present.
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

// SendToPPTAgent enqueues a message from the voice agent to the ppt agent.
func (s *AppState) SendToPPTAgent(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceToPPTQueue = append(s.voiceToPPTQueue, data)
}

// DrainVoiceToPPTQueue removes and returns all voice-agent-to-ppt-agent messages.
func (s *AppState) DrainVoiceToPPTQueue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.voiceToPPTQueue))
	copy(out, s.voiceToPPTQueue)
	s.voiceToPPTQueue = s.voiceToPPTQueue[:0]
	return out
}

// VoiceToPPTQueueLen returns the number of pending voice-agent-to-ppt-agent messages.
func (s *AppState) VoiceToPPTQueueLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.voiceToPPTQueue)
}

// SendToVoiceAgent enqueues a message from the ppt agent to the voice agent.
func (s *AppState) SendToVoiceAgent(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pptToVoiceQueue = append(s.pptToVoiceQueue, data)
}

// DrainPPTToVoiceQueue removes and returns all ppt-agent-to-voice-agent messages.
func (s *AppState) DrainPPTToVoiceQueue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.pptToVoiceQueue))
	copy(out, s.pptToVoiceQueue)
	s.pptToVoiceQueue = s.pptToVoiceQueue[:0]
	return out
}

// ResetConversation clears all state for a fresh conversation.
func (s *AppState) ResetConversation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.req = model.Requirements{}
	s.requirementsFinalized = false
	s.pptToVoiceQueue = s.pptToVoiceQueue[:0]
	s.voiceToPPTQueue = s.voiceToPPTQueue[:0]
}
