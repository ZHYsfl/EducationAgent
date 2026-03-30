package handlers

import (
	"strings"

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
	if strings.TrimSpace(req.TaskID) == "" ||
		strings.TrimSpace(req.ViewingPageID) == "" ||
		req.BaseTimestamp <= 0 ||
		strings.TrimSpace(req.RawText) == "" {
		contract.Error(c, contract.CodeInvalidParam, "missing required fields for feedback")
		return
	}
	for _, intent := range req.Intents {
		if !isValidActionType(intent.ActionType) {
			contract.Error(c, contract.CodeInvalidParam, "invalid action_type")
			return
		}
	}
	_, err := h.feedbackService.Handle(c.Request.Context(), req)
	if err != nil {
		if err == repository.ErrTaskNotFound || err == repository.ErrPageNotFound {
			contract.Error(c, contract.CodeTaskNotFound, "task/page not found")
			return
		}
		if err == service.ErrInvalidReplyContext || err == service.ErrContextNotMatched || err == service.ErrNoSuspendedConflict {
			contract.Error(c, contract.CodeInvalidParam, err.Error())
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, nil, "success")
}

func (h *FeedbackHandler) TickTimeout(c *gin.Context) {
	if err := h.feedbackService.ProcessTimeoutTick(c.Request.Context()); err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, gin.H{"ok": true}, "")
}

// GeneratePages runs merge+write for multiple pages concurrently.
func (h *FeedbackHandler) GeneratePages(c *gin.Context) {
	var req model.BatchGeneratePagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}

	resp, err := h.feedbackService.GeneratePages(c.Request.Context(), req)
	if err != nil {
		if err == service.ErrInvalidBatchGenerateRequest {
			contract.Error(c, contract.CodeInvalidParam, err.Error())
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}

	contract.Success(c, resp, "success")
}

func isValidActionType(actionType string) bool {
	actionType = strings.TrimSpace(strings.ToLower(actionType))
	switch actionType {
	case "modify", "insert", "delete", "reorder", "style":
		return true
	default:
		return false
	}
}
