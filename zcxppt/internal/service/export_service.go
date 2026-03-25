package service

import (
	"context"
	"fmt"
	"strings"

	"zcxppt/internal/infra/oss"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

type ExportService struct {
	exportRepo repository.ExportRepository
	ossClient  *oss.Client
}

func NewExportService(exportRepo repository.ExportRepository, ossClient *oss.Client) *ExportService {
	return &ExportService{exportRepo: exportRepo, ossClient: ossClient}
}

func (s *ExportService) Create(taskID, format string) (model.ExportCreateResponse, error) {
	job, err := s.exportRepo.Create(taskID, format)
	if err != nil {
		return model.ExportCreateResponse{}, err
	}

	job.Status = "generating"
	job.Progress = 50
	_, _ = s.exportRepo.Update(job)

	ctx := context.Background()
	if strings.EqualFold(format, "docx") {
		content := []byte(fmt.Sprintf("# Task %s\n\nGenerated markdown export for docx-compatible flow.\n", taskID))
		url, size, err := s.ossClient.PutObject(ctx, job.ExportID+".md", content)
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			_, _ = s.exportRepo.Update(job)
			return model.ExportCreateResponse{ExportID: job.ExportID, Status: "failed", EstimatedSeconds: 30}, nil
		}
		job.DownloadURL = url
		job.FileSize = size
	} else {
		content := []byte("mock export content")
		url, size, err := s.ossClient.PutObject(ctx, job.ExportID+"."+strings.ToLower(format), content)
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			_, _ = s.exportRepo.Update(job)
			return model.ExportCreateResponse{ExportID: job.ExportID, Status: "failed", EstimatedSeconds: 30}, nil
		}
		job.DownloadURL = url
		job.FileSize = size
	}

	job.Status = "completed"
	job.Progress = 100
	_, _ = s.exportRepo.Update(job)
	return model.ExportCreateResponse{ExportID: job.ExportID, Status: "generating", EstimatedSeconds: 30}, nil
}

func (s *ExportService) Get(exportID string) (model.ExportStatusResponse, error) {
	job, err := s.exportRepo.Get(exportID)
	if err != nil {
		return model.ExportStatusResponse{}, err
	}
	return model.ExportStatusResponse{
		ExportID:    job.ExportID,
		Status:      job.Status,
		DownloadURL: job.DownloadURL,
		Format:      job.Format,
		FileSize:    job.FileSize,
		Error:       job.Error,
	}, nil
}
