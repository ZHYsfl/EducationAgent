package service

import (
	"errors"
	"strings"

	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

var ErrInvalidVADRequest = errors.New("invalid vad event request")

type PPTService struct {
	taskRepo repository.TaskRepository
	pptRepo  repository.PPTRepository
	feedback repository.FeedbackRepository
}

func NewPPTService(taskRepo repository.TaskRepository, pptRepo repository.PPTRepository, feedbackRepo repository.FeedbackRepository) *PPTService {
	return &PPTService{taskRepo: taskRepo, pptRepo: pptRepo, feedback: feedbackRepo}
}

func (s *PPTService) Init(req model.PPTInitRequest) (string, error) {
	task, err := s.taskRepo.Create(model.Task{
		SessionID: strings.TrimSpace(req.SessionID),
		Topic:     strings.TrimSpace(req.Topic),
		Status:    "created",
		Progress:  10,
	})
	if err != nil {
		return "", err
	}
	_, err = s.pptRepo.InitCanvas(task.TaskID, req.TotalPages)
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
	status, err := s.pptRepo.GetCanvasStatus(strings.TrimSpace(taskID))
	if err != nil {
		return model.CanvasStatusResponse{}, err
	}
	for i := range status.PagesInfo {
		status.PagesInfo[i].Status = normalizePageStatus(status.PagesInfo[i].Status)
	}
	return status, nil
}

func (s *PPTService) GetPageRender(taskID, pageID string) (model.PageRenderResponse, error) {
	page, err := s.pptRepo.GetPageRender(strings.TrimSpace(taskID), strings.TrimSpace(pageID))
	if err != nil {
		return model.PageRenderResponse{}, err
	}
	page.Status = normalizePageStatus(page.Status)
	return page, nil
}

func (s *PPTService) HandleVADEvent(req model.VADEventRequest) error {
	taskID := strings.TrimSpace(req.TaskID)
	pageID := strings.TrimSpace(req.ViewingPageID)
	if taskID == "" || pageID == "" {
		return ErrInvalidVADRequest
	}
	if req.Timestamp <= 0 {
		return ErrInvalidVADRequest
	}
	if _, err := s.pptRepo.GetPageRender(taskID, pageID); err != nil {
		return err
	}
	_, suspended, err := s.feedback.GetSuspend(taskID, pageID)
	if err != nil {
		return err
	}
	if suspended {
		if err := s.feedback.ResolveSuspend(taskID, pageID); err != nil {
			return err
		}
	}
	return nil
}

func normalizePageStatus(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	switch status {
	case "rendering", "completed", "failed", "suspended_for_human":
		return status
	default:
		return "rendering"
	}
}
