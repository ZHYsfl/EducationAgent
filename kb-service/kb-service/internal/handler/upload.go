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
//
// 响应符合 API_DOCUMENTATION.md §5.1：
//   {file_id, filename, file_type, file_size, storage_url, purpose}
func (h *DocumentHandler) UploadDocument(c *gin.Context) {
	const maxFileSize = 500 * 1024 * 1024 // 500 MB，与系统接口规范一致

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

	// 读取文件大小（BUG 3.3 修复：fh.Size 从未读取）
	if fh.Size > maxFileSize {
		util.Fail(c, util.CodeParamError, fmt.Sprintf("文件大小 %d 超过限制 %d 字节", fh.Size, maxFileSize))
		return
	}

	// ── 查集合（获取 user_id）────────────────────────────────────────────────
	coll, err := h.pg.GetCollection(collID)
	if err != nil || coll == nil {
		util.Fail(c, util.CodeNotFound, "collection_id 不存在")
		return
	}
	userID := coll.UserID

	// ── 文件去重：同一用户下同一文件名不能重复上传（BUG 4.5 修复）────────────
	filename := fh.Filename
	if exists, _ := h.pg.FileExistsForUser(userID, filename); exists {
		util.Fail(c, util.CodeConflict, "相同文件已上传，请勿重复上传")
		return
	}

	// ── 生成文档 ID 和存储 key ───────────────────────────────────────────────
	docID := util.NewID("doc_")
	ext := strings.ToLower(filepath.Ext(filename))
	key := fmt.Sprintf("%s/%s/%s%s", userID, docID, docID, ext)

	// ── 写入本地 OSS ─────────────────────────────────────────────────────────
	f, err := fh.Open()
	if err != nil {
		util.Fail(c, util.CodeInternalError, "打开上传文件失败: "+err.Error())
		return
	}

	fileURL, err := h.oss.Put(c.Request.Context(), key, f)
	f.Close()
	if err != nil {
		util.Fail(c, util.CodeInternalError, "文件存储失败: "+err.Error())
		return
	}

	// ── 创建文档元数据记录 ────────────────────────────────────────────────────
	// BUG 5.5 修复：使用 CreateDocumentFull 传入 userID（之前调用 CreateDocument 导致 userID=""）
	title := c.PostForm("title")
	if title == "" {
		title = filename
	}
	now := time.Now().UnixMilli()
	doc := &model.KBDocument{
		DocID:        docID,
		CollectionID: collID,
		FileID:       docID, // BUG 3.6 修复：使用独立 docID，不再用存储路径作 file_id
		Title:        title,
		DocType:      fileType,
		ChunkCount:   0,
		Status:       "processing",
		CreatedAt:    now,
	}
	if err := h.pg.CreateDocumentFull(doc, userID, fileURL); err != nil {
		// BUG 3.5 修复：OSS 成功但 DB 失败 → 清理 OSS 文件，避免孤立对象
		_ = h.oss.Delete(c.Request.Context(), key)
		util.Fail(c, util.CodeInternalError, "创建文档记录失败: "+err.Error())
		return
	}

	// ── 提交异步索引 ──────────────────────────────────────────────────────────
	h.w.Submit(worker.IndexJob{
		DocID:        docID,
		CollectionID: collID,
		UserID:       userID,
		FileURL:      fileURL,
		FileType:     fileType,
		Title:        title,
	})

	// BUG 3.2 修复：响应字段符合 API 文档规范 §5.1
	util.OK(c, gin.H{
		"file_id":     docID,
		"filename":    filename,
		"file_type":   fileType,
		"file_size":   fh.Size,
		"storage_url": fileURL,
		"purpose":     "reference",
	})
}
