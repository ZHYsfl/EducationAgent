package service

import (
	"educationagent/internal/state"
)

// VoiceService handles voice-agent-side business logic.
type VoiceService struct {
	state *state.AppState
}

// NewVoiceService creates a new voice service.
func NewVoiceService(state *state.AppState) *VoiceService {
	return &VoiceService{state: state}
}

// UpdateRequirements proxies to the state store.
func (s *VoiceService) UpdateRequirements(req map[string]any) ([]string, error) {
	return s.state.UpdateRequirements(req)
}

// RequireConfirm proxies to the state store.
func (s *VoiceService) RequireConfirm() error {
	return s.state.RequireConfirm()
}

// SendToPPTAgent enqueues voice-agent data into the voice message queue.
func (s *VoiceService) SendToPPTAgent(data string) {
	s.state.SendToPPTAgent(data)
}

// FetchFromPPTMessageQueue proxies to the state store.
func (s *VoiceService) FetchFromPPTMessageQueue() (string, error) {
	return s.state.FetchFromPPTMessageQueue()
}

