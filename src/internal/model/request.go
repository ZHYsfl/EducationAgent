package model

// UpdateRequirementsRequest is the body for POST /api/v1/update_requirements.
type UpdateRequirementsRequest struct {
	Requirements map[string]any `json:"requirements"`
}

// RequireConfirmRequest is the body for POST /api/v1/require_confirm.
type RequireConfirmRequest struct {
	Requirements Requirements `json:"requirements"`
}

// SendToPPTAgentRequest is the body for POST /api/v1/send_to_ppt_agent.
type SendToPPTAgentRequest struct {
	Data string `json:"data"`
}

// StartConversationRequest is the body for POST /api/v1/start_conversation.
type StartConversationRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}
