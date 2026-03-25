package handlers

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"zcxppt/internal/contract"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
	"zcxppt/internal/service"
)

type TaskHandler struct {
	taskService *service.TaskService
}

func NewTaskHandler(taskService *service.TaskService) *TaskHandler {
	return &TaskHandler{taskService: taskService}
}

func (h *TaskHandler) Create(c *gin.Context) {
	var req model.CreateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	task, err := h.taskService.CreateTask(req)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, gin.H{"task_id": task.TaskID}, "")
}

func (h *TaskHandler) Get(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	task, err := h.taskService.GetTask(taskID)
	if err != nil {
		if err == repository.ErrTaskNotFound {
			contract.Error(c, contract.CodeTaskNotFound, "task not found")
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, task, "")
}

func (h *TaskHandler) UpdateStatus(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	var req model.UpdateTaskStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		contract.Error(c, contract.CodeInvalidParam, "invalid request body")
		return
	}
	task, err := h.taskService.UpdateTaskStatus(taskID, req)
	if err != nil {
		if err == repository.ErrTaskNotFound {
			contract.Error(c, contract.CodeTaskNotFound, "task not found")
			return
		}
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, task, "")
}

func (h *TaskHandler) List(c *gin.Context) {
	sessionID := c.Query("session_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	items, total, err := h.taskService.ListTasks(sessionID, page, pageSize)
	if err != nil {
		contract.Error(c, contract.CodeInternalError, err.Error())
		return
	}
	contract.Success(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	}, "")
}
