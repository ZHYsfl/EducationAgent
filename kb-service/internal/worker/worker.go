// Package worker 实现异步文档索引 worker。
// 流程：接收 IndexJob -> 解析 -> 切块 -> 向量化 -> 写入 Qdrant -> 更新元数据
//
// 生产级特性：
//   - 重试机制：失败任务自动重试（默认 3 次，指数退避）
//   - Dead Letter Queue：超过重试上限的任务持久化到 PostgreSQL DLQ 表
//   - 持久化：DLQ 中的任务在服务重启后自动重放
//   - 优雅关闭：收到信号后等待正在执行的任务完成
package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"kb-service/internal/model"
	"kb-service/internal/parser"
	"kb-service/internal/store"
	"kb-service/pkg/util"
)

// IndexJob = store.IndexJob（避免类型重复定义）
type IndexJob = store.IndexJob

// IndexWorker 异步索引 worker
type IndexWorker struct {
	queue      chan IndexJob
	meta       store.MetaStore
	vec        store.VecStore
	p          parser.Parser
	embedder   parser.Embedder
	maxRetries int
	queueSize  int
	wg         sync.WaitGroup
	running    bool
	mu         sync.Mutex
}

// NewIndexWorker 创建 worker；concurrency 为并发协程数
func NewIndexWorker(
	meta store.MetaStore,
	vec store.VecStore,
	p parser.Parser,
	emb parser.Embedder,
	queueSize int,
	concurrency int,
	maxRetries int,
) *IndexWorker {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	w := &IndexWorker{
		queue:      make(chan IndexJob, queueSize),
		meta:       meta,
		vec:        vec,
		p:          p,
		embedder:   emb,
		maxRetries: maxRetries,
		queueSize:  queueSize,
	}
	for i := 0; i < concurrency; i++ {
		w.wg.Add(1)
		go w.run()
	}
	w.mu.Lock()
	w.running = true
	w.mu.Unlock()
	return w
}

// Submit 提交索引任务（非阻塞，队列满时写入 DB DLQ）
func (w *IndexWorker) Submit(job IndexJob) {
	job.Retry = 0
	select {
	case w.queue <- job:
		log.Printf("[IndexWorker] queued job doc_id=%s", job.DocID)
	default:
		// 队列满，写入数据库 DLQ
		if err := w.meta.DLQPush(job); err != nil {
			log.Printf("[IndexWorker] queue full + DLQ persist failed doc_id=%s: %v", job.DocID, err)
		}
	}
}

// run 消费队列中的任务
func (w *IndexWorker) run() {
	defer w.wg.Done()
	for job := range w.queue {
		w.processWithRetry(job)
	}
}

// processWithRetry 带重试的任务处理
func (w *IndexWorker) processWithRetry(job IndexJob) {
	backoff := time.Second
	var lastErr error

	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		job.Retry = attempt
		err := w.processOnce(job)
		if err == nil {
			return
		}
		lastErr = err
		log.Printf("[IndexWorker] attempt %d/%d failed doc=%s: %v",
			attempt+1, w.maxRetries+1, job.DocID, err)

		if attempt < w.maxRetries {
			time.Sleep(backoff)
			backoff *= 2 // 指数退避：1s, 2s, 4s...，上限60s
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
		}
	}

	// 超过重试上限，写入数据库 DLQ（持久化，进程崩溃不丢失）
	log.Printf("[IndexWorker] retries exhausted doc=%s, pushing to DB DLQ: %v", job.DocID, lastErr)
	if err := w.meta.DLQPush(job); err != nil {
		log.Printf("[IndexWorker] DLQ persist failed doc=%s: %v (last error: %v)", job.DocID, err, lastErr)
	}
}

// processOnce 单次任务处理（无重试逻辑）
func (w *IndexWorker) processOnce(job IndexJob) error {
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
		// BUG 6.2 修复：UpdateDocumentStatus 失败时记录错误（不再静默忽略）
		if statusErr := w.meta.UpdateDocumentStatus(job.DocID, "failed", err.Error(), 0); statusErr != nil {
			log.Printf("[IndexWorker] UpdateDocumentStatus failed: %v (original: %v)", statusErr, err)
		}
		return fmt.Errorf("parse failed: %w", err)
	}

	// 2. 向量化
	texts := make([]string, len(parsed.TextChunks))
	for i, c := range parsed.TextChunks {
		texts[i] = c.Content
	}
	vectors, err := w.embedder.Embed(ctx, texts)
	if err != nil {
		if statusErr := w.meta.UpdateDocumentStatus(job.DocID, "failed", err.Error(), 0); statusErr != nil {
			log.Printf("[IndexWorker] UpdateDocumentStatus failed: %v (original: %v)", statusErr, err)
		}
		return fmt.Errorf("embed failed: %w", err)
	}
	if len(vectors) != len(parsed.TextChunks) {
		msg := fmt.Sprintf("embed returned %d vectors for %d chunks", len(vectors), len(parsed.TextChunks))
		if statusErr := w.meta.UpdateDocumentStatus(job.DocID, "failed", msg, 0); statusErr != nil {
			log.Printf("[IndexWorker] UpdateDocumentStatus failed: %v (original: %v)", statusErr, msg)
		}
		return fmt.Errorf("%s", msg)
	}

	// 3. 写入 Qdrant 向量库
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
		if meta.CreatedAt == 0 {
			meta.CreatedAt = time.Now().UnixMilli()
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
		if statusErr := w.meta.UpdateDocumentStatus(job.DocID, "failed", err.Error(), 0); statusErr != nil {
			log.Printf("[IndexWorker] UpdateDocumentStatus failed: %v (original: %v)", statusErr, err)
		}
		return fmt.Errorf("upsert failed: %w", err)
	}

	// 4. 更新状态 indexed
	if err := w.meta.UpdateDocumentStatus(job.DocID, "indexed", "", len(parsed.TextChunks)); err != nil {
		log.Printf("[IndexWorker] UpdateDocumentStatus(indexed) failed: %v", err)
	}
	// 5. 集合文档计数 +1
	if err := w.meta.IncrDocCount(job.CollectionID, time.Now().UnixMilli()); err != nil {
		log.Printf("[IndexWorker] IncrDocCount failed: %v", err)
	}
	// 6. 内容去重指纹：索引成功后写入 SHA-256(content) 指纹
	//    Content 字段在 handler 层已预计算为 hex 字符串；web_snippet 等无预计算的跳过
	if len(job.Content) == 64 { // SHA-256 hex = 64 chars
		if err := w.meta.RecordContentHash(job.UserID, job.Content, job.DocID, time.Now().UnixMilli()); err != nil {
			log.Printf("[IndexWorker] RecordContentHash failed: %v", err)
		}
	}
	log.Printf("[IndexWorker] indexed doc=%s chunks=%d", job.DocID, len(parsed.TextChunks))
	return nil
}

// DrainDLQ 从数据库重放 DLQ 中的任务（服务启动时调用）
func (w *IndexWorker) DrainDLQ() {
	for {
		jobs, err := w.meta.DLQPop(w.queueSize)
		if err != nil {
			log.Printf("[IndexWorker] DLQ drain error: %v", err)
			return
		}
		if len(jobs) == 0 {
			return
		}
		log.Printf("[IndexWorker] draining %d DLQ jobs", len(jobs))
		for i, job := range jobs {
			select {
			case w.queue <- job:
				log.Printf("[IndexWorker] DLQ replayed doc_id=%s", job.DocID)
			default:
				// BUG 2.6 修复：队列满时，仅将当前失败的任务（index i）写回 DLQ
				// 不再把已成功入队的 job（index 0）也重新写回，导致重复处理
				for idx := i; idx < len(jobs); idx++ {
					_ = w.meta.DLQPush(jobs[idx]) // 忽略错误
				}
				log.Printf("[IndexWorker] DLQ drain queue full, put %d jobs back", len(jobs)-i)
				return
			}
		}
	}
}

// QueueLen 返回当前队列积压任务数
func (w *IndexWorker) QueueLen() int {
	return len(w.queue)
}

// Shutdown 优雅关闭：停止接收新任务，等待正在执行的任务完成
func (w *IndexWorker) Shutdown(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	w.mu.Unlock()

	close(w.queue)

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("[IndexWorker] shutdown complete")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown timeout: %w", ctx.Err())
	}
}
