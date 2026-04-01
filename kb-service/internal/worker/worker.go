// Package worker 实现异步文档索引 worker。
// 流程：接收 IndexJob -> 解析 -> 切块 -> 向量化 -> 写入 Qdrant -> 更新元数据
package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"kb-service/internal/model"
	"kb-service/internal/parser"
	"kb-service/internal/store"
	"kb-service/pkg/util"
)

// IndexJob 索引任务
type IndexJob struct {
	DocID        string
	CollectionID string
	UserID       string
	FileURL      string // 文件对象存储 URL（pdf/docx/pptx/image/video）
	Content      string // 纯文本内容（web_snippet 场景直接传内容，避免语义混乱）
	FileType     string
	Title        string
}

// IndexWorker 异步索引 worker
type IndexWorker struct {
	queue    chan IndexJob
	meta     store.MetaStore
	vec      store.VecStore
	p        parser.Parser
	embedder parser.Embedder
}

// NewIndexWorker 创建 worker；concurrency 为并发协程数
func NewIndexWorker(
	meta store.MetaStore,
	vec store.VecStore,
	p parser.Parser,
	emb parser.Embedder,
	queueSize int,
	concurrency int,
) *IndexWorker {
	w := &IndexWorker{
		queue:    make(chan IndexJob, queueSize),
		meta:     meta,
		vec:      vec,
		p:        p,
		embedder: emb,
	}
	for i := 0; i < concurrency; i++ {
		go w.run()
	}
	return w
}

// Submit 提交索引任务（非阻塞，队列满时打日志丢弃）
func (w *IndexWorker) Submit(job IndexJob) {
	select {
	case w.queue <- job:
	default:
		log.Printf("[IndexWorker] queue full, dropping job doc_id=%s", job.DocID)
	}
}

func (w *IndexWorker) run() {
	for job := range w.queue {
		w.process(job)
	}
}

func (w *IndexWorker) process(job IndexJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. 解析文档
	parsed, err := w.p.Parse(ctx, model.ParseInput{
		FileURL:  job.FileURL,
		Content:  job.Content,
		FileType: job.FileType,
		DocID:    job.DocID,
	})
	if err != nil {
		log.Printf("[IndexWorker] parse failed doc=%s: %v", job.DocID, err)
		_ = w.meta.UpdateDocumentStatus(job.DocID, "failed", err.Error(), 0)
		return
	}

	// 3. 向量化
	texts := make([]string, len(parsed.TextChunks))
	for i, c := range parsed.TextChunks {
		texts[i] = c.Content
	}
	vectors, err := w.embedder.Embed(ctx, texts)
	if err != nil {
		log.Printf("[IndexWorker] embed failed doc=%s: %v", job.DocID, err)
		_ = w.meta.UpdateDocumentStatus(job.DocID, "failed", err.Error(), 0)
		return
	}
	// 校验向量数量与 chunks 数量一致，防止 index out of range
	if len(vectors) != len(parsed.TextChunks) {
		msg := fmt.Sprintf("embed returned %d vectors for %d chunks", len(vectors), len(parsed.TextChunks))
		log.Printf("[IndexWorker] mismatch doc=%s: %s", job.DocID, msg)
		_ = w.meta.UpdateDocumentStatus(job.DocID, "failed", msg, 0)
		return
	}

	// 4. 写入 Qdrant 向量库
	cvs := make([]store.ChunkVector, 0, len(parsed.TextChunks))
	for i, c := range parsed.TextChunks {
		cid := c.ChunkID
		if cid == "" {
			cid = util.NewID("chunk_")
		}
		meta := c.Metadata
		if job.FileType == "web_snippet" {
			meta.Origin = "web_search"
			meta.SourceType = "text"
		}
		cvs = append(cvs, store.ChunkVector{
			ChunkID:      cid,
			DocID:        job.DocID,
			DocTitle:     job.Title,
			CollectionID: job.CollectionID,
			UserID:       job.UserID,
			Content:      c.Content,
			Vector:       vectors[i],
			Metadata:     meta,
		})
	}
	if err := w.vec.UpsertChunks(ctx, cvs); err != nil {
		log.Printf("[IndexWorker] upsert failed doc=%s: %v", job.DocID, err)
		_ = w.meta.UpdateDocumentStatus(job.DocID, "failed", err.Error(), 0)
		return
	}

	// 5. 更新状态 indexed
	_ = w.meta.UpdateDocumentStatus(job.DocID, "indexed", "", len(parsed.TextChunks))
	// 6. 集合文档计数 +1
	_ = w.meta.IncrDocCount(job.CollectionID, time.Now().UnixMilli())
	log.Printf("[IndexWorker] indexed doc=%s chunks=%d", job.DocID, len(parsed.TextChunks))
}
