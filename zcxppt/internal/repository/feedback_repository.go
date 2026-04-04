package repository

import (
	"sync"
	"time"

	"zcxppt/internal/model"
)

type FeedbackRepository interface {
	EnqueuePending(taskID, pageID string, item model.PendingFeedback) error
	DequeuePending(taskID, pageID string) (model.PendingFeedback, bool, error)
	ListPending(taskID, pageID string) ([]model.PendingFeedback, error)
	SetSuspend(state model.SuspendState) error
	GetSuspend(taskID, pageID string) (model.SuspendState, bool, error)
	ResolveSuspend(taskID, pageID string) error
	ListExpiredSuspends(now time.Time) ([]model.SuspendState, error)
}

type InMemoryFeedbackRepository struct {
	mu       sync.Mutex
	pending  map[string][]model.PendingFeedback
	suspends map[string]model.SuspendState
}

func NewInMemoryFeedbackRepository() *InMemoryFeedbackRepository {
	return &InMemoryFeedbackRepository{
		pending:  make(map[string][]model.PendingFeedback),
		suspends: make(map[string]model.SuspendState),
	}
}

func key(taskID, pageID string) string { return taskID + ":" + pageID }

func (r *InMemoryFeedbackRepository) EnqueuePending(taskID, pageID string, item model.PendingFeedback) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending[key(taskID, pageID)] = append(r.pending[key(taskID, pageID)], item)
	return nil
}

func (r *InMemoryFeedbackRepository) DequeuePending(taskID, pageID string) (model.PendingFeedback, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(taskID, pageID)
	queue := r.pending[k]
	if len(queue) == 0 {
		return model.PendingFeedback{}, false, nil
	}
	item := queue[0]
	r.pending[k] = queue[1:]
	return item, true, nil
}

func (r *InMemoryFeedbackRepository) ListPending(taskID, pageID string) ([]model.PendingFeedback, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	queue := r.pending[key(taskID, pageID)]
	cp := make([]model.PendingFeedback, len(queue))
	copy(cp, queue)
	return cp, nil
}

func (r *InMemoryFeedbackRepository) SetSuspend(state model.SuspendState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.suspends[key(state.TaskID, state.PageID)] = state
	return nil
}

func (r *InMemoryFeedbackRepository) GetSuspend(taskID, pageID string) (model.SuspendState, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.suspends[key(taskID, pageID)]
	return s, ok, nil
}

func (r *InMemoryFeedbackRepository) ResolveSuspend(taskID, pageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(taskID, pageID)
	s, ok := r.suspends[k]
	if !ok {
		return nil
	}
	s.Resolved = true
	r.suspends[k] = s
	return nil
}

func (r *InMemoryFeedbackRepository) ListExpiredSuspends(now time.Time) ([]model.SuspendState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	nowMs := now.UnixMilli()
	out := make([]model.SuspendState, 0)
	for _, s := range r.suspends {
		if !s.Resolved && s.ExpiresAt <= nowMs {
			out = append(out, s)
		}
	}
	return out, nil
}
