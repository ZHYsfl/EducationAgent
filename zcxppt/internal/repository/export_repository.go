package repository

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"zcxppt/internal/model"
)

type ExportRepository interface {
	Create(taskID, format string) (model.ExportJob, error)
	Get(exportID string) (model.ExportJob, error)
	Update(job model.ExportJob) (model.ExportJob, error)
}

type InMemoryExportRepository struct {
	mu   sync.RWMutex
	jobs map[string]model.ExportJob
}

func NewInMemoryExportRepository() *InMemoryExportRepository {
	return &InMemoryExportRepository{jobs: make(map[string]model.ExportJob)}
}

func (r *InMemoryExportRepository) Create(taskID, format string) (model.ExportJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	job := model.ExportJob{
		ExportID:  "exp_" + uuid.NewString(),
		TaskID:    taskID,
		Format:    format,
		Status:    "queued",
		Progress:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.jobs[job.ExportID] = job
	return job, nil
}

func (r *InMemoryExportRepository) Get(exportID string) (model.ExportJob, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	job, ok := r.jobs[exportID]
	if !ok {
		return model.ExportJob{}, ErrTaskNotFound
	}
	return job, nil
}

func (r *InMemoryExportRepository) Update(job model.ExportJob) (model.ExportJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job.UpdatedAt = time.Now().UnixMilli()
	r.jobs[job.ExportID] = job
	return job, nil
}
