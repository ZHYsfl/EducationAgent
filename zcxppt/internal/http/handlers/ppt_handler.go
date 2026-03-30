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
	if strings.TrimSpace(req.UserID) == "" ||
		strings.TrimSpace(req.SessionID) == "" ||
		strings.TrimSpace(req.Topic) == "" ||
		strings.TrimSpace(req.Subject) == "" ||
		strings.TrimSpace(req.Description) == "" ||
		req.TotalPages <= 0 ||
		strings.TrimSpace(req.Audience) == "" ||
		strings.TrimSpace(req.GlobalStyle) == "" {
		contract.Error(c, contract.CodeInvalidParam, "missing required fields for ppt init")
		return
	}
	if req.TeachingElements == nil || !validateTeachingElements(*req.TeachingElements) {
		contract.Error(c, contract.CodeInvalidParam, "invalid teaching_elements")
		return
	}
	if !validateReferenceFiles(req.ReferenceFiles) {
		contract.Error(c, contract.CodeInvalidParam, "invalid reference_files")
		return
	}
	taskID, err := h.pptService.Init(req)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, gin.H{"task_id": taskID, "status": "created"}, "success")
}

func validateTeachingElements(el model.InitTeachingElements) bool {
	return len(el.KnowledgePoints) > 0 &&
		len(el.TeachingGoals) > 0 &&
		strings.TrimSpace(el.TeachingLogic) != "" &&
		len(el.KeyDifficulties) > 0 &&
		strings.TrimSpace(el.Duration) != "" &&
		strings.TrimSpace(el.InteractionDesign) != "" &&
		len(el.OutputFormats) > 0
}

func validateReferenceFiles(files []model.ReferenceFile) bool {
	for _, f := range files {
		if strings.TrimSpace(f.FileID) == "" ||
			strings.TrimSpace(f.FileURL) == "" ||
			strings.TrimSpace(f.FileType) == "" ||
			strings.TrimSpace(f.Instruction) == "" {
			return false
		}
	}
	return true
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
	contract.Success(c, status, "success")
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
	contract.Success(c, render, "success")
}

func (h *PPTHandler) VADEvent(c *gin.Context) {
	var req model.VADEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	if err := h.pptService.HandleVADEvent(req); err != nil {
		if err == service.ErrInvalidVADRequest {
			contract.Error(c, contract.CodeInvalidParam, err.Error())
			return
		}
		if err == repository.ErrTaskNotFound || err == repository.ErrPageNotFound {
			contract.Error(c, contract.CodeTaskNotFound, "task/page not found")
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	c.JSON(200, gin.H{"code": contract.CodeSuccess, "message": "success"})
}
