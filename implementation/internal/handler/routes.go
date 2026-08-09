package handler

import (
	"educationagent/internal/service"
	"educationagent/internal/state"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes wires all API handlers to the gin engine.
func RegisterRoutes(
	r *gin.Engine,
	st *state.AppState,
	armSvc *service.ArmService,
	asr service.ASRService,
	interrupt service.InterruptService,
	voiceAgent service.VoiceAgentService,
) {
	r.POST("/api/v1/send_to_arm_agent", VoiceSendToArmAgent(armSvc))
	r.POST("/api/v1/get_message_from_arm_agent", VoiceGetMessageFromArmAgent(st))
	r.POST("/api/v1/send_to_voice_agent", ArmSendToVoiceAgent(armSvc))
	r.POST("/api/v1/start_conversation", StartConversation(st, armSvc))
	r.POST("/api/v1/voice/vad_start", VoiceVADStart(st, asr, interrupt))
	r.POST("/api/v1/voice/vad_end", VoiceVADEnd(st, asr, voiceAgent))
	r.POST("/api/v1/voice/text_input", VoiceTextInput(st, voiceAgent))
	r.GET("/api/v1/arm/log-stream", ArmLogStream(st))
}
