package handler

import (
	"github.com/gin-gonic/gin"
	"educationagent/internal/middleware"
	"educationagent/internal/model"
	"educationagent/internal/service"
)

// VoiceUpdateRequirements handles POST /api/v1/update_requirements
func VoiceUpdateRequirements(voiceSvc *service.VoiceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.UpdateRequirementsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		missing, err := voiceSvc.UpdateRequirements(req.Requirements)
		if err != nil {
			middleware.Fail(c, 400, "failed to update the requirements,please try again")
			return
		}

		var data *model.UpdateRequirementsData
		if len(missing) > 0 {
			data = &model.UpdateRequirementsData{MissingFields: missing}
		} else {
			data = &model.UpdateRequirementsData{MissingFields: nil}
		}
		middleware.OK(c, data)
	}
}

// VoiceRequireConfirm handles POST /api/v1/require_confirm
func VoiceRequireConfirm(voiceSvc *service.VoiceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.RequireConfirmRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		if err := voiceSvc.RequireConfirm(); err != nil {
			middleware.Fail(c, 400, "failed to send the data to the frontend")
			return
		}
		middleware.OK(c, nil)
	}
}

// VoiceSendToPPTAgent handles POST /api/v1/send_to_ppt_agent
func VoiceSendToPPTAgent(voiceSvc *service.VoiceService, pptSvc *service.PPTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.SendToPPTAgentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		if err := pptSvc.OnVoiceMessage(req.Data); err != nil {
			middleware.Fail(c, 400, "failed to send the data to the ppt agent")
			return
		}
		middleware.OK(c, nil)
	}
}

// VoiceFetchFromPPTQueue handles GET /api/v1/fetch_from_ppt_message_queue
func VoiceFetchFromPPTQueue(voiceSvc *service.VoiceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		msg, ok := voiceSvc.FetchFromPPTMessageQueue()
		if !ok {
			middleware.OK(c, nil)
			return
		}
		middleware.OK(c, msg)
	}
}

// StartConversation handles POST /api/v1/start_conversation
func StartConversation(voiceSvc *service.VoiceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.StartConversationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			middleware.Fail(c, 400, "invalid request body")
			return
		}

		voiceSvc.StartConversation()
		middleware.OK(c, nil)
	}
}
