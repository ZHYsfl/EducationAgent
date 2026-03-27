// Package store Qdrant 向量层实现 VecStore 接口。
//
// 依赖：Qdrant（docker: qdrant/qdrant，端口 6333 HTTP / 6334 gRPC）
// 使用 HTTP REST API，无需额外 gRPC 依赖，兼容性更好。
//
// Collection 命名：kb_chunks（全局单集合，通过 payload 过滤 user_id/collection_id）
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"kb-service/internal/model"
)

const (
	qdrantCollection = "kb_chunks"
	vecDimQdrant     = 1024 // BAAI/bge-m3 维度
)

// QdrantStore Qdrant 向量检索层
type QdrantStore struct {
	baseURL string // 如 http://localhost:6333
	client  *http.Client
}

// NewQdrantStore 创建 Qdrant 客户端并确保集合存在
func NewQdrantStore(baseURL string) (*QdrantStore, error) {
	qs := &QdrantStore{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	if err := qs.ensureCollection(context.Background()); err != nil {
		return nil, fmt.Errorf("ensure qdrant collection: %w", err)
	}
	return qs, nil
}

func (q *QdrantStore) Close() error { return nil }

// ensureCollection 幂等创建向量集合（HNSW + Cosine）
func (q *QdrantStore) ensureCollection(ctx context.Context) error {
	// 先检查是否已存在
	resp, err := q.do(ctx, http.MethodGet,
		fmt.Sprintf("/collections/%s", qdrantCollection), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil // 已存在
	}

	// 创建集合
	body := map[string]any{
		"vectors": map[string]any{
			"size":     vecDimQdrant,
			"distance": "Cosine",
			"hnsw_config": map[string]any{
				"m":              16,
				"ef_construct":   100,
				"full_scan_threshold": 10000,
			},
		},
	}
	resp2, err := q.do(ctx, http.MethodPut,
		fmt.Sprintf("/collections/%s", qdrantCollection), body)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("create collection status %d: %s", resp2.StatusCode, b)
	}
	return nil
}

// UpsertChunks 批量写入向量 chunk
func (q *QdrantStore) UpsertChunks(ctx context.Context, chunks []ChunkVector) error {
	if len(chunks) == 0 {
		return nil
	}

	points := make([]map[string]any, 0, len(chunks))
	for _, c := range chunks {
		points = append(points, map[string]any{
			"id":     c.ChunkID,
			"vector": c.Vector,
			"payload": map[string]any{
				"doc_id":        c.DocID,
				"doc_title":     c.DocTitle,
				"collection_id": c.CollectionID,
				"user_id":       c.UserID,
				"content":       c.Content,
				"source_type":   c.Metadata.SourceType,
				"chunk_index":   c.Metadata.ChunkIndex,
				"page_number":   c.Metadata.PageNumber,
				"section_title": c.Metadata.SectionTitle,
				"image_url":     c.Metadata.ImageURL,
				"origin":        c.Metadata.Origin,
				"source_url":    c.Metadata.SourceURL,
			},
		})
	}

	body := map[string]any{"points": points}
	resp, err := q.do(ctx, http.MethodPut,
		fmt.Sprintf("/collections/%s/points?wait=true", qdrantCollection), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upsert points status %d: %s", resp.StatusCode, b)
	}
	return nil
}

// SearchChunks KNN 向量语义检索
func (q *QdrantStore) SearchChunks(ctx context.Context, req SearchChunksReq) ([]model.RetrievedChunk, error) {
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	if topK > 20 {
		topK = 20
	}

	// 构造 payload 过滤条件
	filterMust := []map[string]any{
		{"key": "user_id", "match": map[string]any{"value": req.UserID}},
	}
	if req.CollectionID != "" {
		filterMust = append(filterMust, map[string]any{
			"key": "collection_id", "match": map[string]any{"value": req.CollectionID},
		})
	}

	body := map[string]any{
		"vector":       req.Vector,
		"limit":        topK,
		"with_payload": true,
		"filter":       map[string]any{"must": filterMust},
		"score_threshold": req.ScoreThreshold,
	}

	resp, err := q.do(ctx, http.MethodPost,
		fmt.Sprintf("/collections/%s/points/search", qdrantCollection), body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search status %d: %s", resp.StatusCode, b)
	}

	var result struct {
		Result []struct {
			ID      any                    `json:"id"`
			Score   float64                `json:"score"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search result: %w", err)
	}

	chunks := make([]model.RetrievedChunk, 0, len(result.Result))
	for _, r := range result.Result {
		p := r.Payload
		chunks = append(chunks, model.RetrievedChunk{
			ChunkID:  fmt.Sprintf("%v", r.ID),
			DocID:    strVal(p, "doc_id"),
			DocTitle: strVal(p, "doc_title"),
			Content:  strVal(p, "content"),
			Score:    r.Score,
			Metadata: model.ChunkMeta{
				SourceType:   strVal(p, "source_type"),
				ChunkIndex:   intVal(p, "chunk_index"),
				PageNumber:   intVal(p, "page_number"),
				SectionTitle: strVal(p, "section_title"),
				ImageURL:     strVal(p, "image_url"),
				Origin:       strVal(p, "origin"),
				SourceURL:    strVal(p, "source_url"),
			},
		})
	}
	return chunks, nil
}

// DeleteChunksByDocID 删除某文档的所有向量 chunk
func (q *QdrantStore) DeleteChunksByDocID(ctx context.Context, docID string) error {
	body := map[string]any{
		"filter": map[string]any{
			"must": []map[string]any{
				{"key": "doc_id", "match": map[string]any{"value": docID}},
			},
		},
	}
	resp, err := q.do(ctx, http.MethodPost,
		fmt.Sprintf("/collections/%s/points/delete?wait=true", qdrantCollection), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete points status %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ── HTTP 辅助 ─────────────────────────────────────────────────────────────────

func (q *QdrantStore) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return q.client.Do(req)
}

// strVal 安全从 payload map 取字符串值
func strVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// intVal 安全从 payload map 取整数值（Qdrant 数字返回 float64）
func intVal(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}
