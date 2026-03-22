package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"auth_memory_service/internal/model"
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

func (r *WorkingMemoryRepository) Save(ctx context.Context, wm model.WorkingMemory) error {
	payload, err := json.Marshal(wm)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key(wm.SessionID), payload, r.ttl).Err()
}

func (r *WorkingMemoryRepository) Get(ctx context.Context, sessionID string) (*model.WorkingMemory, error) {
	raw, err := r.client.Get(ctx, r.key(sessionID)).Result()
	if err == redis.Nil {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var wm model.WorkingMemory
	if err := json.Unmarshal([]byte(raw), &wm); err != nil {
		return nil, err
	}
	return &wm, nil
}
