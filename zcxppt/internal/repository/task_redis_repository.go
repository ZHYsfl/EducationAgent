package repository

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"zcxppt/internal/model"
)

type RedisTaskRepository struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisTaskRepository(client *redis.Client, ttl time.Duration) *RedisTaskRepository {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &RedisTaskRepository{client: client, ttl: ttl}
}

func (r *RedisTaskRepository) taskKey(taskID string) string {
	return "task:" + taskID
}

func (r *RedisTaskRepository) sessionIndexKey(sessionID string) string {
	return "tasks:session:" + sessionID
}

func (r *RedisTaskRepository) Create(task model.Task) (model.Task, error) {
	ctx := context.Background()
	now := time.Now().UTC()
	if task.TaskID == "" {
		task.TaskID = "task_" + uuid.NewString()
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	task.CreatedAt = now
	task.UpdatedAt = now
	if err := r.saveTask(ctx, task); err != nil {
		return model.Task{}, err
	}
	if task.SessionID != "" {
		_ = r.client.ZAdd(ctx, r.sessionIndexKey(task.SessionID), redis.Z{Score: float64(task.CreatedAt.UnixMilli()), Member: task.TaskID}).Err()
		_ = r.client.Expire(ctx, r.sessionIndexKey(task.SessionID), r.ttl).Err()
	}
	return task, nil
}

func (r *RedisTaskRepository) GetByID(taskID string) (model.Task, error) {
	ctx := context.Background()
	v, err := r.client.Get(ctx, r.taskKey(taskID)).Result()
	if err == redis.Nil {
		return model.Task{}, ErrTaskNotFound
	}
	if err != nil {
		return model.Task{}, err
	}
	var t model.Task
	if err := json.Unmarshal([]byte(v), &t); err != nil {
		return model.Task{}, err
	}
	return t, nil
}

func (r *RedisTaskRepository) UpdateStatus(taskID, status string, progress int) (model.Task, error) {
	ctx := context.Background()
	t, err := r.GetByID(taskID)
	if err != nil {
		return model.Task{}, err
	}
	t.Status = status
	t.Progress = progress
	t.UpdatedAt = time.Now().UTC()
	if err := r.saveTask(ctx, t); err != nil {
		return model.Task{}, err
	}
	return t, nil
}

func (r *RedisTaskRepository) ListBySession(sessionID string, page, pageSize int) ([]model.Task, int, error) {
	ctx := context.Background()
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	if sessionID != "" {
		ids, err := r.client.ZRevRange(ctx, r.sessionIndexKey(sessionID), 0, -1).Result()
		if err != nil {
			return nil, 0, err
		}
		total := len(ids)
		start := (page - 1) * pageSize
		if start >= total {
			return []model.Task{}, total, nil
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		items := make([]model.Task, 0, end-start)
		for _, id := range ids[start:end] {
			t, err := r.GetByID(id)
			if err == nil {
				items = append(items, t)
			}
		}
		return items, total, nil
	}

	keys, err := r.client.Keys(ctx, "task:*").Result()
	if err != nil {
		return nil, 0, err
	}
	all := make([]model.Task, 0, len(keys))
	for _, k := range keys {
		v, err := r.client.Get(ctx, k).Result()
		if err != nil {
			continue
		}
		var t model.Task
		if json.Unmarshal([]byte(v), &t) == nil {
			all = append(all, t)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	total := len(all)
	start := (page - 1) * pageSize
	if start >= total {
		return []model.Task{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

func (r *RedisTaskRepository) saveTask(ctx context.Context, task model.Task) error {
	b, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.taskKey(task.TaskID), b, r.ttl).Err()
}
