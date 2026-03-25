package repository

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"zcxppt/internal/model"
)

var ErrTaskNotFound = errors.New("task not found")

type TaskRepository interface {
	Create(task model.Task) (model.Task, error)
	GetByID(taskID string) (model.Task, error)
	UpdateStatus(taskID, status string, progress int) (model.Task, error)
	ListBySession(sessionID string, page, pageSize int) ([]model.Task, int, error)
}

type InMemoryTaskRepository struct {
	mu    sync.RWMutex
	tasks map[string]model.Task
}

func NewInMemoryTaskRepository() *InMemoryTaskRepository {
	return &InMemoryTaskRepository{tasks: make(map[string]model.Task)}
}

func (r *InMemoryTaskRepository) Create(task model.Task) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	if task.TaskID == "" {
		task.TaskID = "task_" + uuid.NewString()
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	task.CreatedAt = now
	task.UpdatedAt = now
	r.tasks[task.TaskID] = task
	return task, nil
}

func (r *InMemoryTaskRepository) GetByID(taskID string) (model.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[taskID]
	if !ok {
		return model.Task{}, ErrTaskNotFound
	}
	return t, nil
}

func (r *InMemoryTaskRepository) UpdateStatus(taskID, status string, progress int) (model.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[taskID]
	if !ok {
		return model.Task{}, ErrTaskNotFound
	}
	t.Status = status
	t.Progress = progress
	t.UpdatedAt = time.Now().UTC()
	r.tasks[taskID] = t
	return t, nil
}

func (r *InMemoryTaskRepository) ListBySession(sessionID string, page, pageSize int) ([]model.Task, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	filtered := make([]model.Task, 0)
	for _, t := range r.tasks {
		if sessionID == "" || t.SessionID == sessionID {
			filtered = append(filtered, t)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].CreatedAt.After(filtered[j].CreatedAt) })

	total := len(filtered)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= total {
		return []model.Task{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return filtered[start:end], total, nil
}
