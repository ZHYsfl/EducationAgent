package redisx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Store struct {
	Client *redis.Client
	TTL    time.Duration
	OK     bool
}

func Connect(url string, ttlSec int) (*Store, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return &Store{OK: false}, err
	}
	c := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		return &Store{OK: false}, err
	}
	ttl := time.Duration(ttlSec) * time.Second
	if ttl <= 0 {
		ttl = 300 * time.Second
	}
	return &Store{Client: c, TTL: ttl, OK: true}, nil
}

func (s *Store) Close() error {
	if s.Client == nil {
		return nil
	}
	return s.Client.Close()
}

func (s *Store) Ping(ctx context.Context) bool {
	if !s.OK || s.Client == nil {
		return false
	}
	return s.Client.Ping(ctx).Err() == nil
}

func canvasKey(taskID string) string { return "canvas:" + taskID }
func snapshotKey(taskID string, ts int64) string {
	return fmt.Sprintf("snapshot:%s:%d", taskID, ts)
}

func (s *Store) SaveCanvasDocument(ctx context.Context, taskID string, doc map[string]any) error {
	if !s.OK {
		return fmt.Errorf("redis 不可用")
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return s.Client.Set(ctx, canvasKey(taskID), string(b), 0).Err()
}

func (s *Store) LoadCanvasDocument(ctx context.Context, taskID string) (map[string]any, error) {
	if !s.OK {
		return nil, nil
	}
	raw, err := s.Client.Get(ctx, canvasKey(taskID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// LoadSnapshotDocument 读取 VAD 快照 snapshot:{task_id}:{ts}（与 Python 一致）。
func (s *Store) LoadSnapshotDocument(ctx context.Context, taskID string, ts int64) (map[string]any, error) {
	if !s.OK || ts <= 0 {
		return nil, nil
	}
	raw, err := s.Client.Get(ctx, snapshotKey(taskID, ts)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Store) VADDeepCopySnapshot(ctx context.Context, taskID string, ts int64, viewingPageID string, liveDoc map[string]any) error {
	if !s.OK {
		return fmt.Errorf("redis 不可用")
	}
	snap := deepCopyMap(liveDoc)
	snap["page_display"] = nil
	delete(snap, "page_display")
	if pages, ok := snap["pages"].(map[string]any); ok {
		for k, v := range pages {
			if pm, ok := v.(map[string]any); ok {
				pages[k] = map[string]any{
					"page_id": pm["page_id"],
					"py_code": pm["py_code"],
					"status":  pm["status"],
				}
			}
		}
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return s.Client.Set(ctx, snapshotKey(taskID, ts), string(b), s.TTL).Err()
}

func deepCopyMap(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func UnavailableReason(s *Store) string {
	if s == nil || !s.OK {
		return "Redis 未连接"
	}
	return ""
}

// PublicMediaURL 与 Python public_media_url 一致。
func PublicMediaURL(base, path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}
