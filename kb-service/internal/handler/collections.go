package handler

import (
	"time"

	"github.com/gin-gonic/gin"
	"kb-service/internal/model"
	"kb-service/internal/store"
	"kb-service/pkg/util"
)

// CollectionHandler 处理集合相关接口
type CollectionHandler struct {
	pg store.MetaStore
}

func NewCollectionHandler(pg store.MetaStore) *CollectionHandler {
	return &CollectionHandler{pg: pg}
}

// CreateCollection POST /api/v1/kb/collections
func (h *CollectionHandler) CreateCollection(c *gin.Context) {
	var req model.CreateCollectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "请求体解析失败: "+err.Error())
		return
	}
	if req.Name == "" {
		util.Fail(c, util.CodeParamError, "name 不能为空")
		return
	}
	if req.Subject == "" {
		util.Fail(c, util.CodeParamError, "subject 不能为空")
		return
	}
	if req.UserID == "" {
		util.Fail(c, util.CodeParamError, "user_id 不能为空")
		return
	}
	now := time.Now().UnixMilli()
	coll := &model.KBCollection{
		CollectionID: util.NewID("coll_"),
		UserID:       req.UserID,
		Name:         req.Name,
		Subject:      req.Subject,
		Description:  req.Description,
		DocCount:     0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.pg.CreateCollection(coll); err != nil {
		util.Fail(c, util.CodeInternalError, "创建集合失败: "+err.Error())
		return
	}
	util.OK(c, gin.H{"collection_id": coll.CollectionID})
}

// ListCollections GET /api/v1/kb/collections?user_id={user_id}
func (h *CollectionHandler) ListCollections(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		util.Fail(c, util.CodeParamError, "user_id 不能为空")
		return
	}
	list, total, err := h.pg.ListCollections(userID)
	if err != nil {
		util.Fail(c, util.CodeInternalError, "查询集合失败: "+err.Error())
		return
	}
	if list == nil {
		list = []model.KBCollection{}
	}
	util.OK(c, gin.H{"collections": list, "total": total})
}

// ListCollectionDocuments GET /api/v1/kb/collections/{collection_id}/documents
func (h *CollectionHandler) ListCollectionDocuments(c *gin.Context) {
	collID := c.Param("collection_id")
	if collID == "" {
		util.Fail(c, util.CodeParamError, "collection_id 不能为空")
		return
	}
	page, pageSize := parsePagination(c)
	list, total, err := h.pg.ListDocumentsByCollection(collID, page, pageSize)
	if err != nil {
		util.Fail(c, util.CodeInternalError, "查询文档列表失败: "+err.Error())
		return
	}
	if list == nil {
		list = []model.KBDocument{}
	}
	util.OK(c, gin.H{
		"documents": list,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
