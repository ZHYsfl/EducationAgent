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

// VadStartRequest is the body for POST /api/v1/voice/vad_start.
type VadStartRequest struct{}

// VadEndRequest is the body for POST /api/v1/voice/vad_end.
type VadEndRequest struct {
	Audio                    string `json:"audio"`
	Format                   string `json:"format"`
	NeedsInterruptedPrefix   bool   `json:"needs_interrupted_prefix"`
	InterruptedAssistantText string `json:"interrupted_assistant_text"`
}

// TextInputRequest is the body for POST /api/v1/voice/text_input.
type TextInputRequest struct {
	Text                     string `json:"text"`
	NeedsInterruptedPrefix   bool   `json:"needs_interrupted_prefix"`
	InterruptedAssistantText string `json:"interrupted_assistant_text"`
}

// TtsFinishedRequest is the body for POST /api/v1/voice/tts_finished.
type TtsFinishedRequest struct{}
