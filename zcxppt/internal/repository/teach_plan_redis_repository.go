package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisTeachPlanRepository struct {
	client *redis.Client
}

func NewRedisTeachPlanRepository(client *redis.Client) *RedisTeachPlanRepository {
	return &RedisTeachPlanRepository{client: client}
}

func (r *RedisTeachPlanRepository) planKey(taskID string) string {
	return "teachplan:content:" + taskID
}

func (r *RedisTeachPlanRepository) snapshotIndexKey(taskID string) string {
	return "teachplan:snaps:idx:" + taskID
}

func (r *RedisTeachPlanRepository) snapshotDataKey(taskID string, ts int64) string {
	return fmt.Sprintf("teachplan:snaps:data:%s:%d", taskID, ts)
}

func (r *RedisTeachPlanRepository) InitPlan(taskID string, planID string, planContentJSON string) error {
	ctx := context.Background()
	// 保存当前教案内容
	if err := r.client.Set(ctx, r.planKey(taskID), planContentJSON, 0).Err(); err != nil {
		return err
	}
	// 保存初始快照
	ts := time.Now().UnixMilli()
	return r.saveSnapshotLocked(ctx, taskID, ts, planContentJSON)
}

func (r *RedisTeachPlanRepository) GetPlan(taskID string) (string, error) {
	ctx := context.Background()
	val, err := r.client.Get(ctx, r.planKey(taskID)).Result()
	if err == redis.Nil {
		return "", errors.New("plan not found for task")
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (r *RedisTeachPlanRepository) SaveSnapshot(taskID string, ts int64) (int64, error) {
	ctx := context.Background()
	now := time.Now().UnixMilli()
	snapshotTs := ts
	if snapshotTs <= 0 {
		snapshotTs = now
	}
	currentContent, err := r.GetPlan(taskID)
	if err != nil {
		currentContent = ""
	}
	return snapshotTs, r.saveSnapshotLocked(ctx, taskID, snapshotTs, currentContent)
}

func (r *RedisTeachPlanRepository) saveSnapshotLocked(ctx context.Context, taskID string, ts int64, content string) error {
	snap := struct {
		TaskID    string `json:"task_id"`
		Timestamp int64  `json:"timestamp"`
		Content   string `json:"content"`
	}{TaskID: taskID, Timestamp: ts, Content: content}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	dataKey := r.snapshotDataKey(taskID, ts)
	if err := r.client.Set(ctx, dataKey, b, 24*time.Hour).Err(); err != nil {
		return err
	}
	idxKey := r.snapshotIndexKey(taskID)
	return r.client.ZAdd(ctx, idxKey, redis.Z{Score: float64(ts), Member: ts}).Err()
}

func (r *RedisTeachPlanRepository) GetSnapshotByTs(taskID string, ts int64) (string, error) {
	ctx := context.Background()
	idxKey := r.snapshotIndexKey(taskID)
	results, err := r.client.ZRangeByScore(ctx, idxKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   fmt.Sprintf("%d", ts),
		Count: 1,
	}).Result()
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "", errors.New("no snapshot before specified timestamp")
	}
	var snapTs int64
	fmt.Sscanf(results[0], "%d", &snapTs)
	dataKey := r.snapshotDataKey(taskID, snapTs)
	b, err := r.client.Get(ctx, dataKey).Result()
	if err != nil {
		return "", err
	}
	var snap struct {
		TaskID    string `json:"task_id"`
		Timestamp int64  `json:"timestamp"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal([]byte(b), &snap); err != nil {
		return "", err
	}
	return snap.Content, nil
}

func (r *RedisTeachPlanRepository) UpdatePlan(taskID string, planContentJSON string) error {
	ctx := context.Background()
	return r.client.Set(ctx, r.planKey(taskID), planContentJSON, 0).Err()
}
