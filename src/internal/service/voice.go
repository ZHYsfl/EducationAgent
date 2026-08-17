package service

import (
	"fmt"
	"strings"

	"educationagent/internal/model"
	"educationagent/internal/state"
)

// VoiceService wraps state for the HTTP handlers.
type VoiceService struct {
	store *state.AppState
}

// NewVoiceService creates a new voice service.
func NewVoiceService(store *state.AppState) *VoiceService {
	return &VoiceService{store: store}
}

// Store returns the underlying application state.
func (s *VoiceService) Store() *state.AppState {
	return s.store
}

// UpdateRequirements merges fields and returns the missing fields.
func (s *VoiceService) UpdateRequirements(req map[string]any) (*model.UpdateRequirementsData, error) {
	missing, err := s.store.UpdateRequirements(req)
	if err != nil {
		return nil, err
	}
	var mf []string
	if len(missing) > 0 {
		mf = missing
	}
	return &model.UpdateRequirementsData{MissingFields: mf}, nil
}

// RequireConfirm validates that all required fields are present.
func (s *VoiceService) RequireConfirm(req model.Requirements) error {
	if req.Topic != nil {
		if _, err := s.store.UpdateRequirements(map[string]any{"topic": *req.Topic}); err != nil {
			return err
		}
	}
	if req.Style != nil {
		if _, err := s.store.UpdateRequirements(map[string]any{"style": *req.Style}); err != nil {
			return err
		}
	}
	if req.TotalPages != nil {
		if _, err := s.store.UpdateRequirements(map[string]any{"total_pages": *req.TotalPages}); err != nil {
			return err
		}
	}
	if req.Audience != nil {
		if _, err := s.store.UpdateRequirements(map[string]any{"audience": *req.Audience}); err != nil {
			return err
		}
	}
	return s.store.RequireConfirm()
}

// SendToPPTAgent enqueues data from the voice agent to the PPT agent.
func (s *VoiceService) SendToPPTAgent(data string) error {
	if data == "" {
		return fmt.Errorf("data is required")
	}
	s.store.SendToPPTAgent(data)
	s.store.MarkRequirementsFinalized()
	return nil
}

// GetMessagesFromPPTAgent drains and returns queued PPT messages.
// If the queue is empty, it returns an empty string.
func (s *VoiceService) GetMessagesFromPPTAgent() (string, error) {
	msgs := s.store.DrainPPTToVoiceQueue()
	if len(msgs) == 0 {
		return "", nil
	}
	return strings.Join(msgs, " | "), nil
}

// StartConversation resets state and marks the conversation as started.
func (s *VoiceService) StartConversation() error {
	s.store.ResetConversation()
	s.store.SetConversationStarted()
	return nil
}
