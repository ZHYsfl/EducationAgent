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
	intentParser    *service.IntentParser
}

func NewFeedbackHandler(feedbackService *service.FeedbackService, intentParser *service.IntentParser) *FeedbackHandler {
	return &FeedbackHandler{feedbackService: feedbackService, intentParser: intentParser}
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

	// 如果 Voice Agent 没有传 Intents，则由 PPT Agent 自己解析 RawText
	if len(req.Intents) == 0 && h.intentParser != nil {
		intents, err := h.intentParser.Parse(c.Request.Context(), req.TaskID, req.ViewingPageID, req.RawText)
		if err != nil {
			contract.Error(c, contract.CodeInternalError, "intent parse failed: "+err.Error())
			return
		}
		if len(intents) == 0 {
			contract.Error(c, contract.CodeInvalidParam, "cannot parse any intent from raw_text")
			return
		}
		req.Intents = intents
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
		if err == service.ErrInvalidReplyContext || err == service.ErrContextNotMatched || err == service.ErrNoSuspendedConflict || err == service.ErrUnsupportedActionType || err == service.ErrTargetPageNotFound {
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
	if strings.TrimSpace(req.TaskID) == "" || req.BaseTimestamp <= 0 ||
		strings.TrimSpace(req.RawText) == "" {
		contract.Error(c, contract.CodeInvalidParam, "missing required fields for batch generate")
		return
	}

	// 如果没有传 Intents，从 RawText 解析
	if len(req.Intents) == 0 && h.intentParser != nil {
		intents, err := h.intentParser.Parse(c.Request.Context(), req.TaskID, "", req.RawText)
		if err != nil {
			contract.Error(c, contract.CodeInternalError, "intent parse failed: "+err.Error())
			return
		}
		if len(intents) == 0 {
			contract.Error(c, contract.CodeInvalidParam, "cannot parse any intent from raw_text")
			return
		}
		req.Intents = intents
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
	case "modify", "insert_before", "insert_after", "delete", "global_modify", "reorder", "resolve_conflict":
		return true
	default:
		return false
	}
}
