package handler

import "github.com/gin-gonic/gin"

// RegisterRoutes wires the Module 1 voice-agent endpoints.
func RegisterRoutes(r *gin.Engine, h *VoiceHandler) {
	v1 := r.Group("/api/v1")
	{
		v1.POST("/update_requirements", h.UpdateRequirements)
		v1.POST("/require_confirm", h.RequireConfirm)
		v1.POST("/send_to_ppt_agent", h.SendToPPTAgent)
		v1.GET("/get_messages_from_ppt_agent", h.GetMessagesFromPPTAgent)
		v1.POST("/start_conversation", h.StartConversation)
	}
}
