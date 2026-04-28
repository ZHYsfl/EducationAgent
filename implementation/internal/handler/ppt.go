package handler

import (
	"github.com/gin-gonic/gin"
	"educationagent/internal/middleware"
	"educationagent/internal/model"
	"educationagent/internal/service"
)

// PPTSendToVoiceAgent handles POST /api/v1/send_to_voice_agent
func PPTSendToVoiceAgent(pptSvc *service.PPTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.SendToVoiceAgentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		if err := pptSvc.SendToVoiceAgent(req.Data); err != nil {
			middleware.Fail(c, 400, "failed to send the data to the voice agent")
			return
		}
		middleware.OK(c, nil)
	}
}
