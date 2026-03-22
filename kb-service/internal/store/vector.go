// Package store 提供基于 Redis Stack（RediSearch）的向量检索层。
// 使用 Redis Hash 存储 chunk + 向量，FT.CREATE 建立 HNSW 索引，
// FT.SEARCH 做 KNN 语义检索，完全替代 Qdrant。
//
// 依赖：Redis Stack（docker: redis/redis-stack:latest）
package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	"kb-service/internal/model"
)

const (
	vecDim       = 1024 // BAAI/bge-m3 维度
	chunkPrefix  = "chunk:"
	vecIndexName = "kb_vec_idx"
)

// ChunkVector 带向量的文本块（写入 Redis）
type ChunkVector struct {
	ChunkID      string
	DocID        string
	DocTitle     string
	CollectionID string
	UserID       string
	Content      string
	Vector       []float32
	Metadata     model.ChunkMeta
}

// SearchChunksReq 语义检索请求
type SearchChunksReq struct {
	Vector         []float32
	UserID         string
	CollectionID   string // 空 = 搜索用户所有集合
	TopK           int
	ScoreThreshold float64
}

// VectorStore Redis Stack 向量检索层，复用 RedisStore 的连接
type VectorStore struct {
	rdb *redis.Client
}

// NewVectorStore 基于已有 RedisStore 创建向量层并确保索引存在
func NewVectorStore(rs *RedisStore) (*VectorStore, error) {
	vs := &VectorStore{rdb: rs.rdb}
	if err := vs.ensureIndex(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure vec index: %w", err)
	}
	return vs, nil
}

// ensureIndex 幂等创建 RediSearch 向量索引
func (v *VectorStore) ensureIndex(ctx context.Context) error {
	err := v.rdb.Do(ctx,
		"FT.CREATE", vecIndexName,
		"ON", "HASH",
		"PREFIX", "1", chunkPrefix,
		"SCHEMA",
		"user_id", "TAG",
		"collection_id", "TAG",
		"doc_id", "TAG",
		"doc_title", "TEXT",
		"content", "TEXT",
		"source_type", "TAG",
		"origin", "TAG",
		"chunk_index", "NUMERIC",
		"page_number", "NUMERIC",
		"section_title", "TEXT",
		"image_url", "TAG",
		"source_url", "TAG",
		"vector", "VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", strconv.Itoa(vecDim),
		"DISTANCE_METRIC", "COSINE",
	).Err()
	if err != nil && !strings.Contains(err.Error(), "Index already exists") {
		return err
	}
	return nil
}

// UpsertChunks 批量写入向量 chunk（HSET）
func (v *VectorStore) UpsertChunks(ctx context.Context, chunks []ChunkVector) error {
	for _, c := range chunks {
		key := chunkPrefix + c.ChunkID
		vecBytes := float32SliceToBytes(c.Vector)
		err := v.rdb.Do(ctx,
			"HSET", key,
			"user_id", c.UserID,
			"collection_id", c.CollectionID,
			"doc_id", c.DocID,
			"doc_title", c.DocTitle,
			"content", c.Content,
			"source_type", c.Metadata.SourceType,
			"chunk_index", c.Metadata.ChunkIndex,
			"page_number", c.Metadata.PageNumber,
			"section_title", c.Metadata.SectionTitle,
			"image_url", c.Metadata.ImageURL,
			"origin", c.Metadata.Origin,
			"source_url", c.Metadata.SourceURL,
			"vector", vecBytes,
		).Err()
		if err != nil {
			return fmt.Errorf("hset chunk %s: %w", c.ChunkID, err)
		}
	}
	return nil
}

// SearchChunks KNN 向量语义检索
// 注意：KBQueryRequest.Filters 字段当前版本暂不支持，接收后忽略，后续迭代补充
func (v *VectorStore) SearchChunks(ctx context.Context, req SearchChunksReq) ([]model.RetrievedChunk, error) {
	filterParts := []string{fmt.Sprintf("@user_id:{%s}", escapeTag(req.UserID))}
	if req.CollectionID != "" {
		filterParts = append(filterParts, fmt.Sprintf("@collection_id:{%s}", escapeTag(req.CollectionID)))
	}
	preFilter := strings.Join(filterParts, " ")
	query := fmt.Sprintf("(%s)=>[KNN %d @vector $vec AS score]", preFilter, req.TopK)
	vecBytes := float32SliceToBytes(req.Vector)

	raw, err := v.rdb.Do(ctx,
		"FT.SEARCH", vecIndexName, query,
		"PARAMS", "2", "vec", vecBytes,
		"SORTBY", "score",
		"DIALECT", "2",
		"LIMIT", "0", strconv.Itoa(req.TopK),
	).Result()
	if err != nil {
		return nil, fmt.Errorf("ft.search: %w", err)
	}
	return parseSearchResult(raw, req.ScoreThreshold)
}

// DeleteChunksByDocID 删除某文档的所有向量 chunk
func (v *VectorStore) DeleteChunksByDocID(ctx context.Context, docID string) error {
	raw, err := v.rdb.Do(ctx,
		"FT.SEARCH", vecIndexName,
		fmt.Sprintf("@doc_id:{%s}", escapeTag(docID)),
		"RETURN", "0",
		"LIMIT", "0", "10000",
		"DIALECT", "2",
	).Result()
	if err != nil {
		return fmt.Errorf("ft.search for delete: %w", err)
	}
	keys := extractKeys(raw)
	if len(keys) == 0 {
		return nil
	}
	return v.rdb.Del(ctx, keys...).Err()
}

// ── 辅助函数 ─────────────────────────────────────────────────────────────────

// float32SliceToBytes 将 []float32 序列化为小端字节（Redis Vector 格式）
func float32SliceToBytes(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, f := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// escapeTag 转义 RediSearch TAG 中的特殊字符
func escapeTag(s string) string {
	r := strings.NewReplacer("-", "\\-", ".", "\\.", "@", "\\@", "!", "\\!")
	return r.Replace(s)
}

// parseSearchResult 解析 FT.SEARCH 返回结果
// 格式：[total, key1, [f1,v1,...], key2, [f1,v1,...], ...]
func parseSearchResult(raw interface{}, scoreThreshold float64) ([]model.RetrievedChunk, error) {
	arr, ok := raw.([]interface{})
	if !ok || len(arr) < 1 {
		return nil, nil
	}
	var result []model.RetrievedChunk
	for i := 1; i+1 < len(arr); i += 2 {
		fields, ok := arr[i+1].([]interface{})
		if !ok {
			continue
		}
		fm := fieldsToMap(fields)

		// RediSearch COSINE 返回距离（0=完全相同），转为相似度
		dist, _ := strconv.ParseFloat(fm["score"], 64)
		score := 1.0 - dist
		if score < scoreThreshold {
			continue
		}

		key, _ := arr[i].(string)
		chunkID := strings.TrimPrefix(key, chunkPrefix)
		chunkIdx, _ := strconv.Atoi(fm["chunk_index"])
		pageNum, _ := strconv.Atoi(fm["page_number"])

		result = append(result, model.RetrievedChunk{
			ChunkID:  chunkID,
			DocID:    fm["doc_id"],
			DocTitle: fm["doc_title"],
			Content:  fm["content"],
			Score:    score,
			Metadata: model.ChunkMeta{
				SourceType:   fm["source_type"],
				ChunkIndex:   chunkIdx,
				PageNumber:   pageNum,
				SectionTitle: fm["section_title"],
				ImageURL:     fm["image_url"],
				Origin:       fm["origin"],
				SourceURL:    fm["source_url"],
			},
		})
	}
	return result, nil
}

// fieldsToMap 将 [k1,v1,k2,v2,...] 转为 map
func fieldsToMap(fields []interface{}) map[string]string {
	m := make(map[string]string, len(fields)/2)
	for i := 0; i+1 < len(fields); i += 2 {
		k, _ := fields[i].(string)
		v, _ := fields[i+1].(string)
		m[k] = v
	}
	return m
}

// extractKeys 从 FT.SEARCH RETURN 0 结果中提取所有 key
func extractKeys(raw interface{}) []string {
	arr, ok := raw.([]interface{})
	if !ok || len(arr) < 1 {
		return nil
	}
	var keys []string
	for i := 1; i < len(arr); i += 2 {
		if k, ok := arr[i].(string); ok {
			keys = append(keys, k)
		}
	}
	return keys
}
