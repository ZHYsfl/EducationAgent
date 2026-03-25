package service

import (
	"strings"

	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

type PPTService struct {
	taskRepo repository.TaskRepository
	pptRepo  repository.PPTRepository
}

func NewPPTService(taskRepo repository.TaskRepository, pptRepo repository.PPTRepository) *PPTService {
	return &PPTService{taskRepo: taskRepo, pptRepo: pptRepo}
}

func (s *PPTService) Init(req model.PPTInitRequest) (string, error) {
	task, err := s.taskRepo.Create(model.Task{
		SessionID: strings.TrimSpace(req.SessionID),
		Topic:     strings.TrimSpace(req.Topic),
		Status:    "generating",
		Progress:  10,
	})
	if err != nil {
		return "", err
	}
	_, err = s.pptRepo.InitCanvas(task.TaskID)
	if err != nil {
		return "", err
	}
	_, err = s.taskRepo.UpdateStatus(task.TaskID, "completed", 100)
	if err != nil {
		return "", err
	}
	return task.TaskID, nil
}

func (s *PPTService) GetCanvasStatus(taskID string) (model.CanvasStatusResponse, error) {
	return s.pptRepo.GetCanvasStatus(strings.TrimSpace(taskID))
}

func (s *PPTService) GetPageRender(taskID, pageID string) (model.PageRenderResponse, error) {
	return s.pptRepo.GetPageRender(strings.TrimSpace(taskID), strings.TrimSpace(pageID))
}
