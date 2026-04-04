package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"kb-service/internal/model"
	"kb-service/internal/storage"
	"kb-service/internal/store"
	"kb-service/internal/worker"
	"kb-service/pkg/util"
)

// DocumentHandler 处理文档相关接口
type DocumentHandler struct {
	pg  store.MetaStore
	vec store.VecStore
	w   *worker.IndexWorker
	oss storage.ObjectStorage
}

func NewDocumentHandler(pg store.MetaStore, vec store.VecStore, w *worker.IndexWorker, oss storage.ObjectStorage) *DocumentHandler {
	return &DocumentHandler{pg: pg, vec: vec, w: w, oss: oss}
}

// IndexDocument POST /api/v1/kb/documents
func (h *DocumentHandler) IndexDocument(c *gin.Context) {
	var req model.IndexDocumentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "请求体解析失败: "+err.Error())
		return
	}
	if req.CollectionID == "" {
		util.Fail(c, util.CodeParamError, "collection_id 不能为空")
		return
	}
	if req.FileID == "" {
		util.Fail(c, util.CodeParamError, "file_id 不能为空")
		return
	}
	if req.FileURL == "" {
		util.Fail(c, util.CodeParamError, "file_url 不能为空")
		return
	}
	if req.FileType == "" {
		util.Fail(c, util.CodeParamError, "file_type 不能为空")
		return
	}

	coll, err := h.pg.GetCollection(req.CollectionID)
	if err != nil || coll == nil {
		util.Fail(c, util.CodeNotFound, "collection_id 不存在")
		return
	}
	userID := coll.UserID

	// ── 文件去重：同一用户下 file_id 不能重复索引 ────────────────────────────
	if exists, _ := h.pg.FileExistsForUser(userID, req.FileID); exists {
		util.Fail(c, util.CodeConflict, "file_id 已存在，请勿重复索引")
		return
	}

	// ── URL 去重：同一用户下 source_url 不能重复索引 ─────────────────────────
	if exists, _ := h.pg.URLExistsForUser(userID, req.FileURL); exists {
		util.Fail(c, util.CodeConflict, "source_url 已存在，请勿重复索引")
		return
	}

	// ── 内容去重：读取文件内容计算 SHA-256 ─────────────────────────────────
	// 从 ObjectStorage 获取文件内容（去掉了文件前缀 /storage/）
	contentKey := req.FileURL
	if len(contentKey) > 9 && contentKey[:9] == "/storage/" {
		contentKey = contentKey[9:]
	}
	rc, err := h.oss.Get(c.Request.Context(), contentKey)
	if err == nil && rc != nil {
		defer rc.Close()
		hashed := sha256.New()
		if data, rerr := io.ReadAll(rc); rerr == nil {
			hashed.Write(data)
			contentHash := hex.EncodeToString(hashed.Sum(nil))
			// 内容指纹存在 → 内容重复
			if exists, _ := h.pg.ContentHashExists(userID, contentHash); exists {
				util.Fail(c, util.CodeConflict, "相同内容已索引，请勿重复上传")
				return
			}
			// 先创建文档记录（此时状态为 processing）
			doc := &model.KBDocument{
				DocID:        util.NewID("doc_"),
				CollectionID: req.CollectionID,
				FileID:       req.FileID,
				Title:        req.Title,
				DocType:      req.FileType,
				ChunkCount:   0,
				Status:       "processing",
				CreatedAt:    time.Now().UnixMilli(),
			}
			if err := h.pg.CreateDocument(doc); err != nil {
				util.Fail(c, util.CodeInternalError, "创建文档记录失败: "+err.Error())
				return
			}
			// 索引成功后再写入内容指纹
			// 注意：指纹在 worker 索引成功后由 worker 写入（避免 worker 失败时留下孤立指纹）
			// 这里先提交 worker 任务即可
			h.w.Submit(worker.IndexJob{
				DocID:        doc.DocID,
				CollectionID: req.CollectionID,
				UserID:       userID,
				FileURL:      req.FileURL,
				FileType:     req.FileType,
				Title:        doc.Title,
				Content:       hex.EncodeToString(hashed.Sum(nil)), // 传递 content hash
			})
			util.OK(c, gin.H{"doc_id": doc.DocID, "status": doc.Status})
			return
		}
	}

	// 无法读取文件内容（本地存储未找到等），走传统路径
	title := req.Title
	if title == "" {
		title = req.FileID
	}
	doc := &model.KBDocument{
		DocID:        util.NewID("doc_"),
		CollectionID: req.CollectionID,
		FileID:       req.FileID,
		Title:        title,
		DocType:      req.FileType,
		ChunkCount:   0,
		Status:       "processing",
		CreatedAt:    time.Now().UnixMilli(),
	}
	if err := h.pg.CreateDocument(doc); err != nil {
		util.Fail(c, util.CodeInternalError, "创建文档记录失败: "+err.Error())
		return
	}

	h.w.Submit(worker.IndexJob{
		DocID:        doc.DocID,
		CollectionID: req.CollectionID,
		UserID:       userID,
		FileURL:      req.FileURL,
		FileType:     req.FileType,
		Title:        doc.Title,
	})

	util.OK(c, gin.H{"doc_id": doc.DocID, "status": doc.Status})
}

// GetDocument GET /api/v1/kb/documents/{doc_id}
func (h *DocumentHandler) GetDocument(c *gin.Context) {
	docID := c.Param("doc_id")
	if docID == "" {
		util.Fail(c, util.CodeParamError, "doc_id 不能为空")
		return
	}
	doc, err := h.pg.GetDocument(docID)
	if err != nil {
		util.Fail(c, util.CodeInternalError, "查询文档失败: "+err.Error())
		return
	}
	if doc == nil {
		util.Fail(c, util.CodeNotFound, "文档不存在")
		return
	}
	util.OK(c, doc)
}

// DeleteDocument DELETE /api/v1/kb/documents/{doc_id}
func (h *DocumentHandler) DeleteDocument(c *gin.Context) {
	docID := c.Param("doc_id")
	if docID == "" {
		util.Fail(c, util.CodeParamError, "doc_id 不能为空")
		return
	}
	doc, err := h.pg.GetDocument(docID)
	if err != nil {
		util.Fail(c, util.CodeInternalError, "查询文档失败: "+err.Error())
		return
	}
	if doc == nil {
		util.Fail(c, util.CodeNotFound, "文档不存在")
		return
	}
	if err := h.vec.DeleteChunksByDocID(c.Request.Context(), docID); err != nil {
		util.Fail(c, util.CodeDependencyUnavailable, "向量删除失败: "+err.Error())
		return
	}
	collID, err := h.pg.DeleteDocument(docID)
	if err != nil {
		util.Fail(c, util.CodeInternalError, "删除文档记录失败: "+err.Error())
		return
	}
	if collID != "" {
		_ = h.pg.DecrDocCount(collID, time.Now().UnixMilli())
	}
	c.JSON(200, gin.H{"code": 200, "message": "deleted"})
}
