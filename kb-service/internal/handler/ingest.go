package handler

import (
	"context"
	"fmt"
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
	if len(req.Items) == 0 {
		util.Fail(c, util.CodeParamError2, "items 不能为空")
		return
	}

	// user_id 可选：有则入个人库，无则入公共库
	userID := req.UserID
	if userID == "" {
		userID = "__public__" // 公共知识库标识
	}

	// collection_id 可选：有则直接使用，无则取用户默认集合
	collID := req.CollectionID
	if collID == "" {
		coll, err := h.pg.GetDefaultCollection(userID)
		if err != nil || coll == nil {
			// 公共库默认集合不存在则创建
			collID, err = h.ensurePublicCollection()
			if err != nil {
				util.Fail(c, util.CodeInternalError, "无可用集合且创建失败: "+err.Error())
				return
			}
		} else {
			collID = coll.CollectionID
		}
	}

	var ingested, skipped, failed int
	var docIDs []string

	for _, item := range req.Items {
		// 跳过无效条目（title/url/content 为空均跳过）
		if item.URL == "" || item.Content == "" {
			skipped++
			continue
		}
		// URL 去重（仅对个人用户生效，公共库跳过）
		if userID != "__public__" {
			exists, err := h.pg.URLExistsForUser(userID, item.URL)
			// BUG 2.3 修复：DB 错误时返回失败，而非静默忽略导致重复导入
			if err != nil {
				util.Fail(c, util.CodeInternalError, "URL 去重检查失败: "+err.Error())
				return
			}
			if exists {
				skipped++
				continue
			}
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
			Status:       "processing",
			CreatedAt:    now,
		}
		srcURL := item.URL
		if userID == "__public__" {
			srcURL = "" // 公共库不写 URL 去重记录
		}
		if err := h.pg.CreateDocumentFull(doc, userID, srcURL); err != nil {
			log.Printf("[IngestFromSearch] create doc err: %v", err)
			failed++
			continue
		}

		// 提交异步索引
		h.w.Submit(worker.IndexJob{
			DocID:        docID,
			CollectionID: collID,
			UserID:       userID,
			FileURL:      "",
			Content:      item.Content,
			FileType:     "web_snippet",
			Title:        title,
		})

		docIDs = append(docIDs, docID)
		ingested++
	}

	// BUG 2.4 修复：全部失败时返回错误，而非全部成功
	if ingested == 0 && failed > 0 {
		util.Fail(c, util.CodeInternalError, fmt.Sprintf("所有 %d 个文档入库失败", failed))
		return
	}

	if docIDs == nil {
		docIDs = []string{}
	}
	util.OK(c, gin.H{
		"ingested": ingested,
		"skipped":  skipped,
		"failed":   failed,
		"doc_ids":  docIDs,
	})
}

// ensurePublicCollection 确保公共知识库默认集合存在
func (h *IngestHandler) ensurePublicCollection() (string, error) {
	collID := "kb_public_default"
	_, err := h.pg.GetCollection(collID)
	if err == nil {
		return collID, nil
	}
	now := time.Now().UnixMilli()
	c := &model.KBCollection{
		CollectionID: collID,
		UserID:       "__public__",
		Name:         "公共知识库",
		Subject:      "公共",
		Description:  "公共知识库，所有用户共享",
		DocCount:     0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.pg.CreateCollection(c); err != nil {
		return "", err
	}
	return collID, nil
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
