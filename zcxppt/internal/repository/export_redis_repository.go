package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"zcxppt/internal/model"
)

type RedisExportRepository struct {
	client *redis.Client
}

func NewRedisExportRepository(client *redis.Client) *RedisExportRepository {
	return &RedisExportRepository{client: client}
}

func (r *RedisExportRepository) key(exportID string) string {
	return "export:job:" + exportID
}

func (r *RedisExportRepository) Create(taskID, format string) (model.ExportJob, error) {
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
	_, err := r.Update(job)
	return job, err
}

func (r *RedisExportRepository) Get(exportID string) (model.ExportJob, error) {
	ctx := context.Background()
	v, err := r.client.Get(ctx, r.key(exportID)).Result()
	if err == redis.Nil {
		return model.ExportJob{}, ErrTaskNotFound
	}
	if err != nil {
		return model.ExportJob{}, err
	}
	var out model.ExportJob
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return model.ExportJob{}, err
	}
	return out, nil
}

func (r *RedisExportRepository) Update(job model.ExportJob) (model.ExportJob, error) {
	ctx := context.Background()
	job.UpdatedAt = time.Now().UnixMilli()
	b, err := json.Marshal(job)
	if err != nil {
		return model.ExportJob{}, err
	}
	if err := r.client.Set(ctx, r.key(job.ExportID), b, 24*time.Hour).Err(); err != nil {
		return model.ExportJob{}, err
	}
	return job, nil
}
