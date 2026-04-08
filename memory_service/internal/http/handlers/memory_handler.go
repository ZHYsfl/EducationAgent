package handlers

import (
	"github.com/gin-gonic/gin"

	"memory_service/internal/contract"
	"memory_service/internal/http/middleware"
	"memory_service/internal/model"
	"memory_service/internal/service"
)

type MemoryHandler struct {
	memoryService *service.MemoryService
}

func NewMemoryHandler(memoryService *service.MemoryService) *MemoryHandler {
	return &MemoryHandler{memoryService: memoryService}
}

type RecallRequest struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	TopK      int    `json:"top_k,omitempty"`
}

type RecallAcceptedResponse struct {
	Accepted bool `json:"accepted"`
}

type ContextPushMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ContextPushRequest struct {
	UserID    string               `json:"user_id"`
	SessionID string               `json:"session_id"`
	Messages  []ContextPushMessage `json:"messages"`
}

type ContextPushAcceptedResponse struct {
	Accepted     bool `json:"accepted"`
	MessageCount int  `json:"message_count"`
}

func (h *MemoryHandler) Extract(c *gin.Context) {
	var req service.MemoryExtractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	resp, err := h.memoryService.Extract(c.Request.Context(), req)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "")
}

func (h *MemoryHandler) AcceptRecall(c *gin.Context) {
	var req RecallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	err := h.memoryService.AcceptRecall(c.Request.Context(), middleware.RequestID(c), service.MemoryRecallRequest{
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Query:     req.Query,
		TopK:      req.TopK,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, RecallAcceptedResponse{Accepted: true}, "success")
}

func (h *MemoryHandler) RecallSync(c *gin.Context) {
	var req service.MemoryRecallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	resp, err := h.memoryService.RecallSync(c.Request.Context(), req)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "")
}

func (h *MemoryHandler) ContextPush(c *gin.Context) {
	var req ContextPushRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	messages := make([]model.ConversationTurn, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, model.ConversationTurn{Role: msg.Role, Content: msg.Content})
	}
	err := h.memoryService.AcceptContextPush(c.Request.Context(), middleware.RequestID(c), service.MemoryContextPushRequest{
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Messages:  messages,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, ContextPushAcceptedResponse{
		Accepted:     true,
		MessageCount: len(req.Messages),
	}, "success")
}

func (h *MemoryHandler) GetProfile(c *gin.Context) {
	userID := c.Param("user_id")
	resp, err := h.memoryService.GetProfile(userID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "")
}

func (h *MemoryHandler) UpdateProfile(c *gin.Context) {
	userID := c.Param("user_id")
	var req service.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	if err := h.memoryService.UpdateProfile(userID, req); err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, map[string]any{}, "success")
}

func (h *MemoryHandler) SaveWorking(c *gin.Context) {
	var req service.SaveWorkingMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	if err := h.memoryService.SaveWorkingMemory(c.Request.Context(), req); err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, map[string]any{}, "success")
}

func (h *MemoryHandler) GetWorking(c *gin.Context) {
	sessionID := c.Param("session_id")
	resp, err := h.memoryService.GetWorkingMemory(c.Request.Context(), sessionID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "")
}

func (h *MemoryHandler) handleError(c *gin.Context, err error) {
	se, ok := err.(*service.ServiceError)
	if !ok {
		contract.Error(c, contract.CodeInternalError, "internal error")
		return
	}
	contract.Error(c, se.Code, se.Message)
}
