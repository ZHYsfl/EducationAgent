package handler

import (
	"net/http"

	"educationagent/internal/middleware"
	"educationagent/internal/model"
	"educationagent/internal/service"

	"github.com/gin-gonic/gin"
)

// VoiceHandler exposes the Module 1 voice-agent endpoints.
type VoiceHandler struct {
	svc *service.VoiceService
}

// NewVoiceHandler creates a new voice handler.
func NewVoiceHandler(svc *service.VoiceService) *VoiceHandler {
	return &VoiceHandler{svc: svc}
}

// UpdateRequirements handles POST /api/v1/update_requirements.
func (h *VoiceHandler) UpdateRequirements(c *gin.Context) {
	var req model.UpdateRequirementsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to update the requirements,please try again")
		return
	}
	data, err := h.svc.UpdateRequirements(req.Requirements)
	if err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to update the requirements,please try again")
		return
	}
	middleware.OK(c, data)
}

// RequireConfirm handles POST /api/v1/require_confirm.
func (h *VoiceHandler) RequireConfirm(c *gin.Context) {
	var req model.RequireConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to send the data,please try again")
		return
	}
	if err := h.svc.RequireConfirm(req.Requirements); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to send the data,please try again")
		return
	}
	middleware.OK[any](c, nil)
}

// SendToPPTAgent handles POST /api/v1/send_to_ppt_agent.
func (h *VoiceHandler) SendToPPTAgent(c *gin.Context) {
	var req model.SendToPPTAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to send the data to the ppt agent")
		return
	}
	if err := h.svc.SendToPPTAgent(req.Data); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to send the data to the ppt agent")
		return
	}
	middleware.OK[any](c, nil)
}

// GetMessagesFromPPTAgent handles GET /api/v1/get_messages_from_ppt_agent.
func (h *VoiceHandler) GetMessagesFromPPTAgent(c *gin.Context) {
	data, err := h.svc.GetMessagesFromPPTAgent()
	if err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to fetch the data from the ppt message queue")
		return
	}
	middleware.OK(c, data)
}

// StartConversation handles POST /api/v1/start_conversation.
func (h *VoiceHandler) StartConversation(c *gin.Context) {
	var req model.StartConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to start the conversation")
		return
	}
	if req.From != "frontend" || req.To != "voice_agent" {
		middleware.Fail(c, http.StatusBadRequest, "failed to start the conversation")
		return
	}
	if err := h.svc.StartConversation(); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "failed to start the conversation")
		return
	}
	middleware.OK[any](c, nil)
}
