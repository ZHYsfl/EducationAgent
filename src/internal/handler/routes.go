package handler

import "github.com/gin-gonic/gin"

// RegisterRoutes wires the Module 1 voice-agent endpoints.
func RegisterRoutes(r *gin.Engine, h *VoiceHandler) {
	voice := r.Group("/api/v1/voice")
	{
		voice.POST("/update_requirements", h.UpdateRequirements)
		voice.POST("/require_confirm", h.RequireConfirm)
		voice.POST("/send_to_ppt_agent", h.SendToPPTAgent)
		voice.GET("/get_messages_from_ppt_agent", h.GetMessagesFromPPTAgent)
		voice.POST("/start_conversation", h.StartConversation)
	}
}
