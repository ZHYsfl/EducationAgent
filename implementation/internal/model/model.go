// Package model defines the request/response DTOs shared by handlers,
// services and tests.
package model

// UniformResponse is the API envelope returned by every REST endpoint.
// HTTP status is always 200; the business code lives in Code.
type UniformResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// SSEChunk is one server-sent event emitted during a voice turn.
type SSEChunk struct {
	Type    string `json:"type"` // user_transcript | tts | action | tool | turn_end
	Text    string `json:"text,omitempty"`
	Payload string `json:"payload,omitempty"`
}

// VADStartRequest is the body of POST /api/v1/voice/vad_start.
type VADStartRequest struct {
	Audio  string `json:"audio"`
	Format string `json:"format"`
}

// VADStartData is the data payload of the vad_start response.
type VADStartData struct {
	Interrupt bool `json:"interrupt"`
}

// VADEndRequest is the body of POST /api/v1/voice/vad_end.
type VADEndRequest struct {
	Audio                   string `json:"audio"`
	Format                  string `json:"format"`
	Text                    string `json:"text"`
	NeedsInterruptedPrefix  bool   `json:"needs_interrupted_prefix"`
	InterruptedAssistantText string `json:"interrupted_assistant_text"`
}

// SendToArmAgentRequest is the body of POST /api/v1/send_to_arm_agent.
// Content is the full natural-language task/change message produced by the
// voice agent, forwarded to the arm agent via message_from_voice_agent_queue.
type SendToArmAgentRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Content string `json:"content"`
}

// SendToVoiceAgentRequest is the body of POST /api/v1/send_to_voice_agent.
// Content is a progress/result/help message from the arm agent, enqueued into
// message_from_arm_agent_queue for the voice agent to consume.
type SendToVoiceAgentRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Content string `json:"content"`
}

// StartConversationRequest is the body of POST /api/v1/start_conversation.
type StartConversationRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}
