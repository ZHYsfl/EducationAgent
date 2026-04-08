package handlers

import (
	"context"
	"log"
	"strings"

	"github.com/gin-gonic/gin"

	"zcxppt/internal/contract"
	"zcxppt/internal/model"
	"zcxppt/internal/service"
)

type ContentDiversityHandler struct {
	contentDiversityService *service.ContentDiversityService
}

func NewContentDiversityHandler(contentDiversityService *service.ContentDiversityService) *ContentDiversityHandler {
	return &ContentDiversityHandler{contentDiversityService: contentDiversityService}
}

// Generate starts async content diversity generation (animation + games).
func (h *ContentDiversityHandler) Generate(c *gin.Context) {
	var req model.ContentDiversityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Topic) == "" {
		contract.Error(c, contract.CodeInvalidParam, "topic is required")
		return
	}

	resp, err := h.contentDiversityService.Generate(c.Request.Context(), req)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, resp, "content diversity generation started")
}

// Status returns the current status and results.
func (h *ContentDiversityHandler) Status(c *gin.Context) {
	resultID := c.Param("result_id")
	if strings.TrimSpace(resultID) == "" {
		contract.Error(c, contract.CodeInvalidParam, "result_id is required")
		return
	}

	resp, err := h.contentDiversityService.GetStatus(resultID)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, resp, "success")
}

// Export exports animation or game content in the specified format.
// This is an asynchronous operation: the service returns immediately and notifies via callback.
func (h *ContentDiversityHandler) Export(c *gin.Context) {
	var req model.ExportContentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	if strings.TrimSpace(req.ResultID) == "" ||
		strings.TrimSpace(req.ContentType) == "" ||
		strings.TrimSpace(req.Format) == "" {
		contract.Error(c, contract.CodeInvalidParam, "result_id, content_type, and format are required")
		return
	}

	contentType := strings.ToLower(req.ContentType)
	if contentType != "animation" && contentType != "game" {
		contract.Error(c, contract.CodeInvalidParam, "content_type must be 'animation' or 'game'")
		return
	}

	format := strings.ToLower(req.Format)
	if format != "html5" && format != "gif" && format != "mp4" {
		contract.Error(c, contract.CodeInvalidParam, "format must be 'html5', 'gif', or 'mp4'")
		return
	}

	// gif/mp4 需要异步执行（耗时较长），html5 可同步
	if format == "html5" {
		var resp model.ExportContentResponse
		var err error
		if contentType == "animation" {
			resp, err = h.contentDiversityService.ExportAnimation(c.Request.Context(), req.ResultID, "", format)
		} else {
			resp, err = h.contentDiversityService.ExportGame(c.Request.Context(), req.ResultID, "", format)
		}
		if err != nil {
			contract.Error(c, contract.CodeInternalError, err.Error())
			return
		}
		contract.Success(c, resp, "export completed")
		return
	}

	// gif/mp4：异步执行，立即返回，后台通知
	go func() {
		var resp model.ExportContentResponse
		var err error
		if contentType == "animation" {
			resp, err = h.contentDiversityService.ExportAnimation(context.Background(), req.ResultID, "", format)
		} else {
			resp, err = h.contentDiversityService.ExportGame(context.Background(), req.ResultID, "", format)
		}
		if err != nil {
			log.Printf("[content_diversity_export] failed: result_id=%s content_type=%s format=%s err=%v",
				req.ResultID, contentType, format, err)
			return
		}
		log.Printf("[content_diversity_export] completed: result_id=%s content_type=%s format=%s url=%s",
			req.ResultID, contentType, format, resp.DownloadURL)
	}()

	contract.Success(c, gin.H{
		"result_id":   req.ResultID,
		"content_type": contentType,
		"format":       format,
		"status":       "generating",
		"message":      "export is processing asynchronously, result will be notified via callback",
	}, "export started")
}

// Integrate embeds animation/game into PPT page code.
func (h *ContentDiversityHandler) Integrate(c *gin.Context) {
	var req model.IntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	if strings.TrimSpace(req.TaskID) == "" || strings.TrimSpace(req.PageID) == "" {
		contract.Error(c, contract.CodeInvalidParam, "task_id and page_id are required")
		return
	}

	resp, err := h.contentDiversityService.Integrate(c.Request.Context(), req)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, resp, "success")
}
