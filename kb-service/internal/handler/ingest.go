package handler

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"kb-service/internal/model"
	"kb-service/internal/parser"
	"kb-service/internal/store"
	"kb-service/internal/worker"
	"kb-service/pkg/util"
)

// IngestHandler 处理搜索结果入库接口
type IngestHandler struct {
	pg       store.MetaStore
	embedder parser.Embedder
	p        parser.Parser
	w        *worker.IndexWorker
}

func NewIngestHandler(
	pg store.MetaStore,
	embedder parser.Embedder,
	p parser.Parser,
	w *worker.IndexWorker,
) *IngestHandler {
	return &IngestHandler{pg: pg, embedder: embedder, p: p, w: w}
}

// IngestFromSearch POST /api/v1/kb/ingest-from-search
// 接收 Web Search 沉淀结果，按 URL 去重后切块入向量库（规范 §4.3.8）
func (h *IngestHandler) IngestFromSearch(c *gin.Context) {
	var req model.IngestFromSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "请求体解析失败: "+err.Error())
		return
	}
	if req.UserID == "" {
		util.Fail(c, util.CodeParamError, "user_id 不能为空")
		return
	}
	if len(req.Items) == 0 {
		util.Fail(c, util.CodeParamError2, "items 不能为空")
		return
	}

	// 自动归入用户默认集合
	collID := req.CollectionID
	if collID == "" {
		coll, err := h.pg.GetDefaultCollection(req.UserID)
		if err != nil || coll == nil {
			util.Fail(c, util.CodeNotFound, "用户没有可用集合，请先创建集合")
			return
		}
		collID = coll.CollectionID
	}

	var ingested, skipped int
	var docIDs []string

	for _, item := range req.Items {
		// 跳过无效条目
		if item.URL == "" || item.Content == "" {
			skipped++
			continue
		}
		// URL 去重
		exists, err := h.pg.URLExistsForUser(req.UserID, item.URL)
		if err != nil {
			log.Printf("[IngestFromSearch] check url exists err: %v", err)
		}
		if exists {
			skipped++
			continue
		}

		// 创建文档记录
		now := time.Now().UnixMilli()
		docID := util.NewID("doc_")
		title := item.Title
		if title == "" {
			title = item.URL
		}
		doc := &model.KBDocument{
			DocID:        docID,
			CollectionID: collID,
			FileID:       "",
			Title:        title,
			DocType:      "web_snippet",
			ChunkCount:   0,
			Status:       "processing", // 与 IndexDocument 保持一致
			CreatedAt:    now,
		}
		if err := h.pg.CreateDocumentFull(doc, req.UserID, item.URL); err != nil {
			log.Printf("[IngestFromSearch] create doc err: %v", err)
			continue
		}

		// 提交异步索引：Content 放入专用字段，FileURL 留空
		h.w.Submit(worker.IndexJob{
			DocID:        docID,
			CollectionID: collID,
			UserID:       req.UserID,
			FileURL:      "",
			Content:      item.Content,
			FileType:     "web_snippet",
			Title:        title,
		})

		docIDs = append(docIDs, docID)
		ingested++
	}

	if docIDs == nil {
		docIDs = []string{}
	}
	util.OK(c, gin.H{
		"ingested": ingested,
		"skipped":  skipped,
		"doc_ids":  docIDs,
	})
}

// ParseHandler 处理纯解析接口
type ParseHandler struct {
	p parser.Parser
}

func NewParseHandler(p parser.Parser) *ParseHandler {
	return &ParseHandler{p: p}
}

// Parse POST /api/v1/kb/parse
// 纯文档解析（不入向量库），供 PPT Agent 等场景复用（规范 §8.5）
func (h *ParseHandler) Parse(c *gin.Context) {
	var req model.ParseInput
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "请求体解析失败: "+err.Error())
		return
	}
	if req.FileURL == "" && req.Content == "" {
		util.Fail(c, util.CodeParamError, "file_url 或 content 至少需要提供一个")
		return
	}
	if req.FileType == "" {
		util.Fail(c, util.CodeParamError, "file_type 不能为空")
		return
	}
	if req.DocID == "" {
		req.DocID = util.NewID("doc_")
	}

	parsed, err := h.p.Parse(context.Background(), req)
	if err != nil {
		util.Fail(c, util.CodeInternalError, "解析失败: "+err.Error())
		return
	}
	util.OK(c, parsed)
}
