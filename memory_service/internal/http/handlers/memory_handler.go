package handlers

import (
	"github.com/gin-gonic/gin"

	"memory_service/internal/contract"
	"memory_service/internal/service"
)

type MemoryHandler struct {
	memoryService *service.MemoryService
}

func NewMemoryHandler(memoryService *service.MemoryService) *MemoryHandler {
	return &MemoryHandler{memoryService: memoryService}
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

func (h *MemoryHandler) Recall(c *gin.Context) {
	var req service.MemoryRecallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeBadRequest, "missing required field")
		return
	}
	resp, err := h.memoryService.Recall(c.Request.Context(), req)
	if err != nil {
		h.handleError(c, err)
		return
	}
	contract.Success(c, resp, "")
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
