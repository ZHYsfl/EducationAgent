package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"zcxppt/internal/model"
)

type RedisFeedbackRepository struct {
	client *redis.Client
}

func NewRedisFeedbackRepository(client *redis.Client) *RedisFeedbackRepository {
	return &RedisFeedbackRepository{client: client}
}

func (r *RedisFeedbackRepository) pendingKey(taskID, pageID string) string {
	return "feedback:pending:" + taskID + ":" + pageID
}

func (r *RedisFeedbackRepository) suspendKey(taskID, pageID string) string {
	return "feedback:suspend:" + taskID + ":" + pageID
}

func (r *RedisFeedbackRepository) suspendIndexKey() string {
	return "feedback:suspend:index"
}

func (r *RedisFeedbackRepository) EnqueuePending(taskID, pageID string, item model.PendingFeedback) error {
	ctx := context.Background()
	b, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return r.client.RPush(ctx, r.pendingKey(taskID, pageID), b).Err()
}

func (r *RedisFeedbackRepository) DequeuePending(taskID, pageID string) (model.PendingFeedback, bool, error) {
	ctx := context.Background()
	v, err := r.client.LPop(ctx, r.pendingKey(taskID, pageID)).Result()
	if err == redis.Nil {
		return model.PendingFeedback{}, false, nil
	}
	if err != nil {
		return model.PendingFeedback{}, false, err
	}
	var out model.PendingFeedback
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return model.PendingFeedback{}, false, err
	}
	return out, true, nil
}

func (r *RedisFeedbackRepository) ListPending(taskID, pageID string) ([]model.PendingFeedback, error) {
	ctx := context.Background()
	vals, err := r.client.LRange(ctx, r.pendingKey(taskID, pageID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]model.PendingFeedback, 0, len(vals))
	for _, v := range vals {
		var item model.PendingFeedback
		if err := json.Unmarshal([]byte(v), &item); err == nil {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *RedisFeedbackRepository) SetSuspend(state model.SuspendState) error {
	ctx := context.Background()
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := r.client.Set(ctx, r.suspendKey(state.TaskID, state.PageID), b, 0).Err(); err != nil {
		return err
	}
	return r.client.ZAdd(ctx, r.suspendIndexKey(), redis.Z{Score: float64(state.ExpiresAt), Member: state.TaskID + ":" + state.PageID}).Err()
}

func (r *RedisFeedbackRepository) GetSuspend(taskID, pageID string) (model.SuspendState, bool, error) {
	ctx := context.Background()
	v, err := r.client.Get(ctx, r.suspendKey(taskID, pageID)).Result()
	if err == redis.Nil {
		return model.SuspendState{}, false, nil
	}
	if err != nil {
		return model.SuspendState{}, false, err
	}
	var out model.SuspendState
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return model.SuspendState{}, false, err
	}
	return out, true, nil
}

func (r *RedisFeedbackRepository) ResolveSuspend(taskID, pageID string) error {
	ctx := context.Background()
	state, ok, err := r.GetSuspend(taskID, pageID)
	if err != nil || !ok {
		return err
	}
	state.Resolved = true
	if err := r.SetSuspend(state); err != nil {
		return err
	}
	return r.client.ZRem(ctx, r.suspendIndexKey(), taskID+":"+pageID).Err()
}

func (r *RedisFeedbackRepository) ListExpiredSuspends(now time.Time) ([]model.SuspendState, error) {
	ctx := context.Background()
	members, err := r.client.ZRangeByScore(ctx, r.suspendIndexKey(), &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(now.UnixMilli(), 10)}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]model.SuspendState, 0, len(members))
	for _, m := range members {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) != 2 {
			continue
		}
		state, ok, err := r.GetSuspend(parts[0], parts[1])
		if err == nil && ok && !state.Resolved && state.ExpiresAt <= now.UnixMilli() {
			out = append(out, state)
		}
	}
	return out, nil
}
