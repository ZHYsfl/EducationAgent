package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"

	"zcxppt/internal/contract"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
	"zcxppt/internal/service"
)

type PPTHandler struct {
	pptService *service.PPTService
}

func NewPPTHandler(pptService *service.PPTService) *PPTHandler {
	return &PPTHandler{pptService: pptService}
}

func (h *PPTHandler) Init(c *gin.Context) {
	var req model.PPTInitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Topic) == "" || strings.TrimSpace(req.Description) == "" {
		contract.Error(c, contract.CodeInvalidParam, "topic or description is required")
		return
	}
	if req.TeachingElements == nil {
		contract.Error(c, contract.CodeInvalidParam, "teaching_elements is required")
		return
	}
	taskID, err := h.pptService.Init(req)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, gin.H{"task_id": taskID}, "")
}

func (h *PPTHandler) CanvasStatus(c *gin.Context) {
	taskID := c.Query("task_id")
	status, err := h.pptService.GetCanvasStatus(taskID)
	if err != nil {
		if err == repository.ErrTaskNotFound {
			contract.Error(c, contract.CodeTaskNotFound, "task not found")
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, status, "")
}

func (h *PPTHandler) PageRender(c *gin.Context) {
	taskID := c.Query("task_id")
	pageID := c.Param("page_id")
	render, err := h.pptService.GetPageRender(taskID, pageID)
	if err != nil {
		if err == repository.ErrTaskNotFound || err == repository.ErrPageNotFound {
			contract.Error(c, contract.CodeTaskNotFound, "task/page not found")
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, render, "")
}
