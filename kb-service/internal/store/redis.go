// Package store 提供基于 Redis 的集合/文档元数据持久化层。
// 数据结构设计：
//   coll:{id}          → Hash，存 KBCollection 字段
//   user_colls:{uid}   → ZSet，score=created_at，member=collection_id（按时间排序）
//   doc:{id}           → Hash，存 KBDocument 字段（含 user_id、source_url）
//   coll_docs:{collid} → ZSet，score=created_at，member=doc_id
//   user_urls:{uid}    → Set，member=source_url（URL 去重）
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"kb-service/internal/model"
)

// RedisStore Redis 持久化层
type RedisStore struct {
	rdb *redis.Client
}

// NewRedisStore 初始化 Redis 连接
func NewRedisStore(addr, password string, db int) (*RedisStore, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		PoolSize:     20,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &RedisStore{rdb: rdb}, nil
}

func (s *RedisStore) Close() error { return s.rdb.Close() }

// ── key 生成 ────────────────────────────────────────────────────────────────

func keyCollection(id string) string   { return "coll:" + id }
func keyUserColls(uid string) string   { return "user_colls:" + uid }
func keyDocument(id string) string     { return "doc:" + id }
func keyCollDocs(collID string) string { return "coll_docs:" + collID }
func keyUserURLs(uid string) string    { return "user_urls:" + uid }

// ── Collections ─────────────────────────────────────────────────────────────

// CreateCollection 写入集合数据
func (s *RedisStore) CreateCollection(c *model.KBCollection) error {
	ctx := context.Background()
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, keyCollection(c.CollectionID), data, 0)
	pipe.ZAdd(ctx, keyUserColls(c.UserID), redis.Z{
		Score:  float64(c.CreatedAt),
		Member: c.CollectionID,
	})
	_, err = pipe.Exec(ctx)
	return err
}

// ListCollections 查询用户所有集合（按 created_at 倒序）
func (s *RedisStore) ListCollections(userID string) ([]model.KBCollection, int, error) {
	ctx := context.Background()
	ids, err := s.rdb.ZRevRange(ctx, keyUserColls(userID), 0, -1).Result()
	if err != nil {
		return nil, 0, err
	}
	var list []model.KBCollection
	for _, id := range ids {
		c, err := s.getCollectionByID(ctx, id)
		if err != nil || c == nil {
			continue
		}
		list = append(list, *c)
	}
	return list, len(list), nil
}

// GetCollection 按 ID 查单个集合
func (s *RedisStore) GetCollection(collID string) (*model.KBCollection, error) {
	return s.getCollectionByID(context.Background(), collID)
}

func (s *RedisStore) getCollectionByID(ctx context.Context, collID string) (*model.KBCollection, error) {
	val, err := s.rdb.Get(ctx, keyCollection(collID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c model.KBCollection
	if err := json.Unmarshal([]byte(val), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// GetDefaultCollection 取用户最早创建的集合（score 最小）
func (s *RedisStore) GetDefaultCollection(userID string) (*model.KBCollection, error) {
	ctx := context.Background()
	ids, err := s.rdb.ZRange(ctx, keyUserColls(userID), 0, 0).Result()
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return s.getCollectionByID(ctx, ids[0])
}

// IncrDocCount 集合文档计数 +1
func (s *RedisStore) IncrDocCount(collID string, now int64) error {
	ctx := context.Background()
	c, err := s.getCollectionByID(ctx, collID)
	if err != nil || c == nil {
		return err
	}
	c.DocCount++
	c.UpdatedAt = now
	data, _ := json.Marshal(c)
	return s.rdb.Set(ctx, keyCollection(collID), data, 0).Err()
}

// DecrDocCount 集合文档计数 -1（不低于0）
func (s *RedisStore) DecrDocCount(collID string, now int64) error {
	ctx := context.Background()
	c, err := s.getCollectionByID(ctx, collID)
	if err != nil || c == nil {
		return err
	}
	if c.DocCount > 0 {
		c.DocCount--
	}
	c.UpdatedAt = now
	data, _ := json.Marshal(c)
	return s.rdb.Set(ctx, keyCollection(collID), data, 0).Err()
}

// ── Documents ───────────────────────────────────────────────────────────────

// docRecord 内部存储结构（含 user_id、source_url，model.KBDocument 不含这两个）
type docRecord struct {
	model.KBDocument
	UserID    string `json:"user_id"`
	SourceURL string `json:"source_url"`
}

// CreateDocument 写入文档元数据
func (s *RedisStore) CreateDocument(d *model.KBDocument) error {
	return s.createDocInternal(context.Background(), d, "", "")
}

// CreateDocumentFull 写入文档元数据（含 user_id、source_url）
func (s *RedisStore) CreateDocumentFull(d *model.KBDocument, userID, sourceURL string) error {
	return s.createDocInternal(context.Background(), d, userID, sourceURL)
}

func (s *RedisStore) createDocInternal(ctx context.Context, d *model.KBDocument, userID, sourceURL string) error {
	rec := docRecord{KBDocument: *d, UserID: userID, SourceURL: sourceURL}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, keyDocument(d.DocID), data, 0)
	pipe.ZAdd(ctx, keyCollDocs(d.CollectionID), redis.Z{
		Score:  float64(d.CreatedAt),
		Member: d.DocID,
	})
	if sourceURL != "" && userID != "" {
		pipe.SAdd(ctx, keyUserURLs(userID), sourceURL)
	}
	_, err = pipe.Exec(ctx)
	return err
}

// GetDocument 按 ID 查文档
func (s *RedisStore) GetDocument(docID string) (*model.KBDocument, error) {
	val, err := s.rdb.Get(context.Background(), keyDocument(docID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec docRecord
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return nil, err
	}
	d := rec.KBDocument
	return &d, nil
}

// UpdateDocumentStatus 更新文档状态/chunk 数/错误信息
func (s *RedisStore) UpdateDocumentStatus(docID, status, errMsg string, chunkCount int) error {
	ctx := context.Background()
	val, err := s.rdb.Get(ctx, keyDocument(docID)).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	var rec docRecord
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return err
	}
	rec.Status = status
	rec.ErrorMessage = errMsg
	rec.ChunkCount = chunkCount
	data, _ := json.Marshal(rec)
	return s.rdb.Set(ctx, keyDocument(docID), data, 0).Err()
}

// DeleteDocument 删除文档记录，返回所属 collection_id
func (s *RedisStore) DeleteDocument(docID string) (string, error) {
	ctx := context.Background()
	val, err := s.rdb.Get(ctx, keyDocument(docID)).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var rec docRecord
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return "", err
	}
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, keyDocument(docID))
	pipe.ZRem(ctx, keyCollDocs(rec.CollectionID), docID)
	if rec.SourceURL != "" && rec.UserID != "" {
		pipe.SRem(ctx, keyUserURLs(rec.UserID), rec.SourceURL)
	}
	_, err = pipe.Exec(ctx)
	return rec.CollectionID, err
}

// ListDocumentsByCollection 分页列出集合内文档（按 created_at 倒序）
func (s *RedisStore) ListDocumentsByCollection(collID string, page, pageSize int) ([]model.KBDocument, int, error) {
	ctx := context.Background()
	total64, err := s.rdb.ZCard(ctx, keyCollDocs(collID)).Result()
	if err != nil {
		return nil, 0, err
	}
	total := int(total64)
	start := int64((page - 1) * pageSize)
	stop := start + int64(pageSize) - 1
	ids, err := s.rdb.ZRevRange(ctx, keyCollDocs(collID), start, stop).Result()
	if err != nil {
		return nil, 0, err
	}
	var list []model.KBDocument
	for _, id := range ids {
		d, err := s.GetDocument(id)
		if err != nil || d == nil {
			continue
		}
		list = append(list, *d)
	}
	return list, total, nil
}

// URLExistsForUser 检查 URL 是否已在用户知识库中（去重）
func (s *RedisStore) URLExistsForUser(userID, sourceURL string) (bool, error) {
	exists, err := s.rdb.SIsMember(context.Background(), keyUserURLs(userID), sourceURL).Result()
	return exists, err
}


