package handlers

import (
	"github.com/gin-gonic/gin"

	"zcxppt/internal/contract"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
	"zcxppt/internal/service"
)

type FeedbackHandler struct {
	feedbackService *service.FeedbackService
}

func NewFeedbackHandler(feedbackService *service.FeedbackService) *FeedbackHandler {
	return &FeedbackHandler{feedbackService: feedbackService}
}

func (h *FeedbackHandler) Feedback(c *gin.Context) {
	var req model.FeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	if len(req.Intents) == 0 {
		contract.Error(c, contract.CodeInvalidParam, "intents is required")
		return
	}
	resp, err := h.feedbackService.Handle(c.Request.Context(), req)
	if err != nil {
		if err == repository.ErrTaskNotFound || err == repository.ErrPageNotFound {
			contract.Error(c, contract.CodeTaskNotFound, "task/page not found")
			return
		}
		if err == service.ErrEmptyIntents || err == service.ErrInvalidReplyContext || err == service.ErrContextNotMatched || err == service.ErrNoSuspendedConflict {
			contract.Error(c, contract.CodeInvalidParam, err.Error())
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, resp, "")
}

func (h *FeedbackHandler) TickTimeout(c *gin.Context) {
	if err := h.feedbackService.ProcessTimeoutTick(c.Request.Context()); err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, gin.H{"ok": true}, "")
}
