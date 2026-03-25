package service

import (
	"strings"

	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

type TaskService struct {
	repo repository.TaskRepository
}

func NewTaskService(repo repository.TaskRepository) *TaskService {
	return &TaskService{repo: repo}
}

func (s *TaskService) CreateTask(req model.CreateTaskRequest) (model.Task, error) {
	task := model.Task{
		SessionID: strings.TrimSpace(req.SessionID),
		Topic:     strings.TrimSpace(req.Topic),
		Status:    "pending",
		Progress:  0,
	}
	return s.repo.Create(task)
}

func (s *TaskService) GetTask(taskID string) (model.Task, error) {
	return s.repo.GetByID(strings.TrimSpace(taskID))
}

func (s *TaskService) UpdateTaskStatus(taskID string, req model.UpdateTaskStatusRequest) (model.Task, error) {
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "pending"
	}
	return s.repo.UpdateStatus(strings.TrimSpace(taskID), status, req.Progress)
}

func (s *TaskService) ListTasks(sessionID string, page, pageSize int) ([]model.Task, int, error) {
	return s.repo.ListBySession(strings.TrimSpace(sessionID), page, pageSize)
}
