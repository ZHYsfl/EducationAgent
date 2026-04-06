package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"memory_service/internal/model"
)

type WorkingMemoryRepository struct {
	client *redis.Client
	ttl    time.Duration
}

func NewWorkingMemoryRepository(client *redis.Client, ttl time.Duration) *WorkingMemoryRepository {
	return &WorkingMemoryRepository{client: client, ttl: ttl}
}

func (r *WorkingMemoryRepository) key(sessionID string) string {
	return fmt.Sprintf("working_mem:%s", sessionID)
}

func (r *WorkingMemoryRepository) Save(ctx context.Context, wm model.WorkingMemoryRecord) error {
	payload, err := json.Marshal(wm)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key(wm.SessionID), payload, r.ttl).Err()
}

func (r *WorkingMemoryRepository) Get(ctx context.Context, sessionID string) (*model.WorkingMemoryRecord, error) {
	raw, err := r.client.Get(ctx, r.key(sessionID)).Result()
	if err == redis.Nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var record model.WorkingMemoryRecord
	if err := json.Unmarshal([]byte(raw), &record); err == nil && record.SessionID != "" {
		return &record, nil
	}
	var legacy model.WorkingMemory
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		return nil, err
	}
	record = model.WorkingMemoryRecord{
		SessionID:           legacy.SessionID,
		UserID:              legacy.UserID,
		ConversationSummary: legacy.ConversationSummary,
		ExtractedElements:   legacy.ExtractedElements,
		RecentTopics:        legacy.RecentTopics,
		UpdatedAt:           legacy.UpdatedAt,
		TaskState: model.WorkingTaskState{
			KnowledgePoints: legacy.ExtractedElements.KnowledgePoints,
			TeachingGoals:   legacy.ExtractedElements.TeachingGoals,
			KeyDifficulties: legacy.ExtractedElements.KeyDifficulties,
			TargetAudience:  legacy.ExtractedElements.TargetAudience,
			Duration:        legacy.ExtractedElements.Duration,
			OutputStyle:     legacy.ExtractedElements.OutputStyle,
		},
		Continuity: model.ContinuityActive,
	}
	return &record, nil
}
