package handler

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"kb-service/internal/model"
	"kb-service/internal/worker"
	"kb-service/pkg/util"
)

// UploadDocument POST /api/v1/kb/upload
// 接收 multipart/form-data 文件上传，保存到本地 OSS，
// 并自动触发异步索引流程（与 IndexDocument 等价，但文件由本服务存储）
//
// 表单字段：
//   file          - 文件二进制内容（必填）
//   collection_id - 目标集合 ID（必填）
//   file_type     - 文件类型 pdf|docx|pptx|image|video|text（必填）
//   title         - 文档标题（可选）
func (h *DocumentHandler) UploadDocument(c *gin.Context) {
	// ── 参数校验 ──────────────────────────────────────────────────────────────
	collID := c.PostForm("collection_id")
	if collID == "" {
		util.Fail(c, util.CodeParamError, "collection_id 不能为空")
		return
	}
	fileType := c.PostForm("file_type")
	if fileType == "" {
		util.Fail(c, util.CodeParamError, "file_type 不能为空")
		return
	}

	// ── 读取上传文件 ──────────────────────────────────────────────────────────
	fh, err := c.FormFile("file")
	if err != nil {
		util.Fail(c, util.CodeParamError, "file 字段缺失: "+err.Error())
		return
	}

	// ── 查集合（获取 user_id）────────────────────────────────────────────────
	coll, err := h.pg.GetCollection(collID)
	if err != nil || coll == nil {
		util.Fail(c, util.CodeNotFound, "collection_id 不存在")
		return
	}

	// ── 生成文档 ID 和存储 key ───────────────────────────────────────────────
	docID := util.NewID("doc_")
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	key := fmt.Sprintf("%s/%s/%s%s", coll.UserID, docID, docID, ext)

	// ── 写入本地 OSS ─────────────────────────────────────────────────────────
	f, err := fh.Open()
	if err != nil {
		util.Fail(c, util.CodeInternalError, "打开上传文件失败: "+err.Error())
		return
	}
	defer f.Close()

	fileURL, err := h.oss.Put(c.Request.Context(), key, f)
	if err != nil {
		util.Fail(c, util.CodeInternalError, "文件存储失败: "+err.Error())
		return
	}

	// ── 创建文档元数据记录 ────────────────────────────────────────────────────
	title := c.PostForm("title")
	if title == "" {
		title = fh.Filename
	}
	now := time.Now().UnixMilli()
	doc := &model.KBDocument{
		DocID:        docID,
		CollectionID: collID,
		FileID:       key, // 本地 OSS key 作为 file_id
		Title:        title,
		DocType:      fileType,
		ChunkCount:   0,
		Status:       "processing",
		CreatedAt:    now,
	}
	if err := h.pg.CreateDocument(doc); err != nil {
		util.Fail(c, util.CodeInternalError, "创建文档记录失败: "+err.Error())
		return
	}

	// ── 提交异步索引 ──────────────────────────────────────────────────────────
	h.w.Submit(worker.IndexJob{
		DocID:        docID,
		CollectionID: collID,
		UserID:       coll.UserID,
		FileURL:      fileURL,
		FileType:     fileType,
		Title:        title,
	})

	util.OK(c, gin.H{
		"doc_id":   docID,
		"file_url": fileURL,
		"status":   "processing",
	})
}
