package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"

	"zcxppt/internal/contract"
	"zcxppt/internal/model"
	"zcxppt/internal/service"
)

type TeachingPlanHandler struct {
	teachingPlanService *service.TeachingPlanService
}

func NewTeachingPlanHandler(teachingPlanService *service.TeachingPlanService) *TeachingPlanHandler {
	return &TeachingPlanHandler{teachingPlanService: teachingPlanService}
}

// Generate starts async Word教案 generation.
func (h *TeachingPlanHandler) Generate(c *gin.Context) {
	var req model.TeachingPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Topic) == "" {
		contract.Error(c, contract.CodeInvalidParam, "topic is required")
		return
	}

	resp, err := h.teachingPlanService.Generate(c.Request.Context(), req)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, resp, "teaching plan generation started")
}

// Status returns the status of a teaching plan generation job.
func (h *TeachingPlanHandler) Status(c *gin.Context) {
	planID := c.Param("plan_id")
	if strings.TrimSpace(planID) == "" {
		contract.Error(c, contract.CodeInvalidParam, "plan_id is required")
		return
	}

	status, err := h.teachingPlanService.GetStatus(planID)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, status, "success")
}
