package handler

import (
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
	oss storage.ObjectStorage // 本地文件存储，用于接收上传的文件
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
		Status:       "processing", // 规范 §4.3.3：立即返回 processing
		CreatedAt:    time.Now().UnixMilli(),
	}
	if err := h.pg.CreateDocument(doc); err != nil {
		util.Fail(c, util.CodeInternalError, "创建文档记录失败: "+err.Error())
		return
	}

	h.w.Submit(worker.IndexJob{
		DocID:        doc.DocID,
		CollectionID: req.CollectionID,
		UserID:       coll.UserID,
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
