// Package store 定义元数据存储和向量存储的抽象接口。
// 具体实现：
//   MetaStore  → postgres.go（PostgreSQL）
//   VectorStore → qdrant.go（Qdrant）
// 切换存储后端只需在 main.go 替换构造函数，上层代码零修改。
package store

import (
	"context"

	"kb-service/internal/model"
)

// MetaStore 元数据持久化接口（集合、文档、URL去重）
type MetaStore interface {
	// ── Collections ──────────────────────────────────────────────────────────
	CreateCollection(c *model.KBCollection) error
	ListCollections(userID string) ([]model.KBCollection, int, error)
	GetCollection(collID string) (*model.KBCollection, error)
	GetDefaultCollection(userID string) (*model.KBCollection, error)
	IncrDocCount(collID string, now int64) error
	DecrDocCount(collID string, now int64) error

	// ── Documents ─────────────────────────────────────────────────────────────
	CreateDocument(d *model.KBDocument) error
	CreateDocumentFull(d *model.KBDocument, userID, sourceURL string) error
	GetDocument(docID string) (*model.KBDocument, error)
	UpdateDocumentStatus(docID, status, errMsg string, chunkCount int) error
	DeleteDocument(docID string) (collID string, err error)
	ListDocumentsByCollection(collID string, page, pageSize int) ([]model.KBDocument, int, error)

	// ── URL 去重 ───────────────────────────────────────────────────────────────
	URLExistsForUser(userID, sourceURL string) (bool, error)

	// ── 生命周期 ───────────────────────────────────────────────────────────────
	Close() error
}

// ChunkVector 带向量的文本块（写入向量库）
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
	CollectionID   string  // 空 = 搜索用户所有集合
	TopK           int
	ScoreThreshold float64
}

// VecStore 向量存储接口（KNN 检索）
// 注意：Filters 字段当前版本暂不支持，接收后忽略，后续迭代补充
type VecStore interface {
	UpsertChunks(ctx context.Context, chunks []ChunkVector) error
	SearchChunks(ctx context.Context, req SearchChunksReq) ([]model.RetrievedChunk, error)
	DeleteChunksByDocID(ctx context.Context, docID string) error
	Close() error
}
