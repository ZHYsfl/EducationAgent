package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"

	"zcxppt/internal/contract"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
	"zcxppt/internal/service"
)

type ExportHandler struct {
	exportService *service.ExportService
}

func NewExportHandler(exportService *service.ExportService) *ExportHandler {
	return &ExportHandler{exportService: exportService}
}

func (h *ExportHandler) Create(c *gin.Context) {
	var req model.ExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.Format) == "" {
		contract.Error(c, contract.CodeInvalidParam, "task_id and format are required")
		return
	}
	resp, err := h.exportService.Create(req.TaskID, req.Format)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, resp, "success")
}

func (h *ExportHandler) Get(c *gin.Context) {
	exportID := c.Param("export_id")
	job, err := h.exportService.Get(exportID)
	if err != nil {
		if err == repository.ErrTaskNotFound {
			contract.Error(c, contract.CodeTaskNotFound, "export not found")
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, job, "")
}
