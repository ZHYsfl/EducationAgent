// Package store 定义元数据存储和向量存储的抽象接口。
// 具体实现：
//   MetaStore  → postgres.go（PostgreSQL）
//   VecStore   → qdrant.go（Qdrant）
// 切换存储后端只需在 main.go 替换构造函数，上层代码零修改。
package store

import (
	"context"

	"kb-service/internal/model"
)

// MetaStore 元数据持久化接口（集合、文档、URL去重、DLQ）
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
	DeleteDocument(docID string) (collId string, err error)
	ListDocumentsByCollection(collID string, page, pageSize int) ([]model.KBDocument, int, error)

	// ── URL 去重 ───────────────────────────────────────────────────────────────
	URLExistsForUser(userID, sourceURL string) (bool, error)

	// ── 文件去重 ─────────────────────────────────────────────────────────────
	// FileExistsForUser 同一用户下 file_id 是否已存在（防止重复索引）
	FileExistsForUser(userID, fileID string) (bool, error)

	// ── 内容去重 ─────────────────────────────────────────────────────────────
	// ContentHashExists 检查同一用户下该内容是否已索引（SHA-256(content) 指纹）
	ContentHashExists(userID, contentHash string) (bool, error)
	// RecordContentHash 索引成功后写入内容指纹
	RecordContentHash(userID, contentHash, docID string, createdAt int64) error

	// ── Dead Letter Queue ─────────────────────────────────────────────────────
	// DLQPush 写入 DLQ（持久化到数据库，进程崩溃不丢失）
	DLQPush(job IndexJob) error
	// DLQPop 取出并删除最早的一批 DLQ 任务（FIFO，用于服务重启后重放）
	DLQPop(count int) ([]IndexJob, error)
	// DLQSize 返回 DLQ 当前积压数量
	DLQSize() (int, error)

	// ── 生命周期 ───────────────────────────────────────────────────────────────
	// Ping 健康检查（用于 /health 和 metrics 采集）
	Ping(ctx context.Context) error
	Close() error
}

// IndexJob 索引任务（store 层用于 DLQ 序列化）
type IndexJob struct {
	DocID        string
	CollectionID string
	UserID       string
	FileURL      string
	Content      string
	FileType     string
	Title        string
	Retry        int
}

// ChunkVector 带向量的文本块（写入向量库）
type ChunkVector struct {
	ChunkID      string
	DocID        string
	DocTitle     string
	CollectionID string
	UserID       string
	Content      string
	Vector       []float32   // Dense 向量（bge-m3 1024维）
	Metadata     model.ChunkMeta
}

// SearchChunksReq 检索请求（支持混合检索、过滤、rerank）
type SearchChunksReq struct {
	Vector         []float32   // Dense 向量
	SparseVector   []uint32    // Sparse BM25 向量（已废弃，保留兼容性）
	SparseValues   []float32   // Sparse 向量对应的 BM25 分数
	QueryText      string      // 原始查询文本（用于 rerank / 混合检索本地 BM25 评分）
	UserID         string
	CollectionID   string      // 空 = 搜索用户所有集合
	TopK           int
	ScoreThreshold float64
	Rerank         bool        // 是否启用 rerank
	Hybrid         bool        // 是否启用混合检索（dense向量 + 本地BM25关键词融合）
	DenseWeight    float32     // Hybrid 时 dense 向量权重（默认 0.5）
	RerankTopK     int         // Rerank 后保留的结果数（默认 20）

	// ── 过滤器 ───────────────────────────────────────────────────────────────
	Filters struct {
		SourceType string  // source_type 精确过滤（text|ocr|video_transcript）
		Origin     string  // origin 精确过滤（web_search|upload）
		DateFrom   int64   // 文档创建时间下限（毫秒时间戳）
		DateTo     int64   // 文档创建时间上限（毫秒时间戳）
	}
}

// VecStore 向量存储接口（KNN 检索，支持 Hybrid 和 Rerank）
type VecStore interface {
	UpsertChunks(ctx context.Context, chunks []ChunkVector) error
	SearchChunks(ctx context.Context, req SearchChunksReq) ([]model.RetrievedChunk, error)
	DeleteChunksByDocID(ctx context.Context, docID string) error
	Close() error
}
