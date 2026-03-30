package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (a *App) uploadFile(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fail(c, 40001, "参数 file 不能为空")
		return
	}
	defer file.Close()

	purpose := strings.TrimSpace(c.PostForm("purpose"))
	if purpose == "" {
		purpose = "reference"
	} else if !isAllowedPurpose(purpose) {
		fail(c, 40001, "参数 purpose 非法")
		return
	}

	userID := userIDFromContext(c)
	if userID == "" {
		fail(c, 40100, "未授权或 token 非法")
		return
	}

	fileID := newID("file_")
	safeName := strings.ReplaceAll(filepath.Base(header.Filename), " ", "_")
	objectKey := fmt.Sprintf("%s/%s/%s_%s", userID, purpose, fileID, safeName)

	hasher := sha256.New()
	teeReader := io.TeeReader(file, hasher)
	if err = a.storage.Upload(c.Request.Context(), objectKey, teeReader, header.Size); err != nil {
		fail(c, 50000, "上传对象存储失败")
		return
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	storageURL := a.publicObjectURL(objectKey)
	if storageURL == "" {
		fail(c, 50000, "生成 storage_url 失败")
		return
	}

	rec := FileModel{
		ID:         fileID,
		UserID:     userID,
		SessionID:  nil,
		TaskID:     nil,
		Filename:   header.Filename,
		FileType:   detectFileType(header.Filename),
		FileSize:   header.Size,
		StorageURL: storageURL,
		ObjectKey:  objectKey,
		Checksum:   checksum,
		Purpose:    purpose,
		CreatedAt:  nowMs(),
	}
	if err = a.db.Create(&rec).Error; err != nil {
		fail(c, 50000, "保存 files 元数据失败")
		return
	}

	ok(c, gin.H{
		"file_id":     rec.ID,
		"filename":    rec.Filename,
		"file_type":   rec.FileType,
		"file_size":   rec.FileSize,
		"storage_url": rec.StorageURL,
		"purpose":     rec.Purpose,
	})
}

func (a *App) getFile(c *gin.Context) {
	fileID := c.Param("file_id")
	if !strings.HasPrefix(fileID, "file_") {
		fail(c, 40001, "file_id 格式非法")
		return
	}

	var rec FileModel
	if err := a.db.First(&rec, "id = ?", fileID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, 40400, "文件不存在")
			return
		}
		fail(c, 50000, "查询文件失败")
		return
	}

	// Ensure storage_url is always a usable URL (older records may have "oss://").
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(rec.StorageURL)), "oss://") || strings.TrimSpace(rec.StorageURL) == "" {
		rec.StorageURL = a.publicObjectURL(rec.ObjectKey)
	}

	reader, err := a.storage.Download(c.Request.Context(), rec.ObjectKey)
	if err != nil {
		fail(c, 50000, "读取对象存储失败")
		return
	}
	h := sha256.New()
	if _, err = io.Copy(h, reader); err != nil {
		reader.Close()
		fail(c, 50000, "校验文件完整性失败")
		return
	}
	_ = reader.Close()
	actualChecksum := hex.EncodeToString(h.Sum(nil))
	if rec.Checksum != "" && !strings.EqualFold(rec.Checksum, actualChecksum) {
		fail(c, 50000, "文件校验失败，疑似被篡改")
		return
	}

	downloadURL, err := a.storage.GenerateSignedURL(rec.ObjectKey, 10*time.Minute)
	if err != nil {
		fail(c, 50000, "生成下载地址失败")
		return
	}

	ok(c, gin.H{
		"file_id":      rec.ID,
		"filename":     rec.Filename,
		"file_type":    rec.FileType,
		"file_size":    rec.FileSize,
		"storage_url":  rec.StorageURL,
		"checksum":     rec.Checksum,
		"download_url": downloadURL,
		"purpose":      rec.Purpose,
		"created_at":   rec.CreatedAt,
	})
}

func (a *App) deleteFile(c *gin.Context) {
	fileID := c.Param("file_id")
	if !strings.HasPrefix(fileID, "file_") {
		fail(c, 40001, "file_id 格式非法")
		return
	}

	var rec FileModel
	if err := a.db.First(&rec, "id = ?", fileID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, 40400, "文件不存在")
			return
		}
		fail(c, 50000, "查询文件失败")
		return
	}

	n := nowMs()
	job := FileDeleteJobModel{ID: newID("fdel_"), FileID: rec.ID, StorageURL: rec.StorageURL, ObjectKey: rec.ObjectKey, Status: "pending", RetryCount: 0, LastError: "", CreatedAt: n, UpdatedAt: n}
	tx := a.db.Begin()
	if err := tx.Create(&job).Error; err != nil {
		tx.Rollback()
		fail(c, 50000, "写入 file_delete_jobs 失败")
		return
	}
	if err := tx.Delete(&FileModel{}, "id = ?", fileID).Error; err != nil {
		tx.Rollback()
		fail(c, 50000, "删除 files 记录失败")
		return
	}
	if err := tx.Commit().Error; err != nil {
		fail(c, 50000, "删除事务提交失败")
		return
	}
	ok(c, gin.H{"deleted": true, "file_id": fileID})
}

func (a *App) createSession(c *gin.Context) {
	var req struct {
		UserID    string `json:"user_id"`
		SessionID string `json:"session_id"`
		Title     string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 40001, "请求体格式错误")
		return
	}
	if req.UserID == "" {
		req.UserID = userIDFromContext(c)
	}
	if req.UserID == "" || !strings.HasPrefix(req.UserID, "user_") {
		fail(c, 40001, "参数 user_id 非法")
		return
	}

	sid := strings.TrimSpace(req.SessionID)
	if sid != "" {
		if !strings.HasPrefix(sid, "sess_") {
			fail(c, 40001, "参数 session_id 非法")
			return
		}
		var existing SessionModel
		if err := a.db.First(&existing, "id = ?", sid).Error; err == nil {
			fail(c, 40900, "会话已存在")
			return
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, 50000, "查询会话失败")
			return
		}
	} else {
		sid = newID("sess_")
	}

	n := nowMs()
	rec := SessionModel{ID: sid, UserID: req.UserID, Title: req.Title, Status: "active", CreatedAt: n, UpdatedAt: n}
	if err := a.db.Create(&rec).Error; err != nil {
		fail(c, 50000, "创建会话失败")
		return
	}
	ok(c, gin.H{
		"session_id": rec.ID,
		"user_id":    rec.UserID,
		"title":      rec.Title,
		"status":     rec.Status,
		"created_at": rec.CreatedAt,
	})
}

func (a *App) getSession(c *gin.Context) {
	sid := c.Param("session_id")
	if !strings.HasPrefix(sid, "sess_") {
		fail(c, 40001, "session_id 格式非法")
		return
	}
	var rec SessionModel
	if err := a.db.First(&rec, "id = ?", sid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, 40400, "会话不存在")
			return
		}
		fail(c, 50000, "查询会话失败")
		return
	}
	ok(c, gin.H{"session_id": rec.ID, "user_id": rec.UserID, "title": rec.Title, "status": rec.Status, "created_at": rec.CreatedAt, "updated_at": rec.UpdatedAt})
}

func (a *App) listSessions(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		userID = userIDFromContext(c)
	}
	if userID == "" || !strings.HasPrefix(userID, "user_") {
		fail(c, 40001, "参数 user_id 非法")
		return
	}

	page := parsePositiveInt(c.DefaultQuery("page", "1"), 1)
	pageSize := parsePositiveInt(c.DefaultQuery("page_size", "20"), 20)
	if pageSize > 100 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize

	var total int64
	if err := a.db.Model(&SessionModel{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		fail(c, 50000, "查询会话总数失败")
		return
	}
	var rows []SessionModel
	if err := a.db.Where("user_id = ?", userID).Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&rows).Error; err != nil {
		fail(c, 50000, "查询会话列表失败")
		return
	}

	items := make([]gin.H, 0, len(rows))
	for _, s := range rows {
		items = append(items, gin.H{"session_id": s.ID, "user_id": s.UserID, "title": s.Title, "status": s.Status, "created_at": s.CreatedAt, "updated_at": s.UpdatedAt})
	}
	ok(c, gin.H{"sessions": items, "total": total, "page": page})
}

func (a *App) updateSession(c *gin.Context) {
	sid := c.Param("session_id")
	if !strings.HasPrefix(sid, "sess_") {
		fail(c, 40001, "session_id 格式非法")
		return
	}

	var req struct {
		Title  string `json:"title,omitempty"`
		Status string `json:"status,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 40001, "请求体格式错误")
		return
	}
	if req.Status != "" && !isAllowedSessionStatus(req.Status) {
		fail(c, 40001, "status 非法")
		return
	}

	var current SessionModel
	if err := a.db.First(&current, "id = ?", sid).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, 40400, "会话不存在")
			return
		}
		fail(c, 50000, "查询会话失败")
		return
	}

	if req.Title == "" && req.Status == "" {
		fail(c, 40001, "title 和 status 不能同时为空")
		return
	}
	if req.Title != "" {
		current.Title = req.Title
	}
	if req.Status != "" {
		current.Status = req.Status
	}
	current.UpdatedAt = nowMs()

	if err := a.db.Model(&SessionModel{}).Where("id = ?", sid).Updates(map[string]interface{}{"title": current.Title, "status": current.Status, "updated_at": current.UpdatedAt}).Error; err != nil {
		fail(c, 50000, "更新会话失败")
		return
	}
	okWithoutData(c)
}

func (a *App) searchQuery(c *gin.Context) {
	var req struct {
		RequestID  string `json:"request_id"`
		UserID     string `json:"user_id"`
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
		Language   string `json:"language"`
		SearchType string `json:"search_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 40001, "请求体格式错误")
		return
	}
	if req.UserID == "" {
		fail(c, 40001, "参数 user_id 必填")
		return
	}
	if req.Query == "" {
		fail(c, 40001, "参数 query 必填")
		return
	}
	if !strings.HasPrefix(req.UserID, "user_") {
		fail(c, 40001, "参数 user_id 非法")
		return
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 10
	}
	if req.MaxResults > 10 {
		req.MaxResults = 10
	}
	if req.RequestID == "" {
		req.RequestID = newID("search_")
	}
	if req.Language == "" {
		req.Language = "zh"
	}
	req.SearchType = normalizeSearchType(req.SearchType)

	var dup SearchRequestModel
	if err := a.db.Where("request_id = ?", req.RequestID).First(&dup).Error; err == nil {
		fail(c, 40001, "request_id 已存在")
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		fail(c, 50000, "查询搜索请求失败")
		return
	}

	n := nowMs()
	rec := SearchRequestModel{
		RequestID: req.RequestID,
		UserID:    req.UserID,
		Query:     req.Query,
		Status:    "pending",
		Results:   "[]",
		Summary:   "",
		Duration:  0,
		CreatedAt: n,
		UpdatedAt: n,
	}
	if err := a.db.Create(&rec).Error; err != nil {
		fail(c, 50000, "保存搜索记录失败")
		return
	}

	go a.runSearchJob(req.RequestID, req.UserID, req.Query, req.MaxResults, req.Language, req.SearchType)

	ok(c, gin.H{
		"request_id": req.RequestID,
		"status":     "pending",
		"results":    []SearchResultItem{},
		"summary":    "",
		"duration":   int64(0),
	})
}

func (a *App) searchResult(c *gin.Context) {
	requestID := strings.TrimSpace(c.Param("request_id"))
	if requestID == "" {
		fail(c, 40001, "request_id 不能为空")
		return
	}
	if !strings.HasPrefix(requestID, "search_") {
		fail(c, 40001, "request_id 格式非法")
		return
	}
	var rec SearchRequestModel
	if err := a.db.First(&rec, "request_id = ?", requestID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fail(c, 40400, "搜索请求不存在")
			return
		}
		fail(c, 50000, "查询搜索结果失败")
		return
	}
	results := make([]SearchResultItem, 0)
	if rec.Results != "" {
		_ = json.Unmarshal([]byte(rec.Results), &results)
	}
	ok(c, gin.H{
		"request_id": rec.RequestID,
		"status":     NormalizeStoredStatusForSection8(rec.Status),
		"results":    results,
		"summary":    rec.Summary,
		"duration":   rec.Duration,
	})
}

// authVerify implements §6.1 认证服务接口：验证用户Token。
func (a *App) authVerify(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 40001, "请求体格式错误")
		return
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		ok(c, gin.H{
			"user_id": "",
			"valid":   false,
		})
		return
	}

	// token 是去掉 "Bearer " 前缀后的内容
	userID := parseUserID("Bearer " + token)

	ok(c, gin.H{
		"user_id": userID,
		"valid":   userID != "",
	})
}

// authProfile implements §6.2 认证服务接口：获取用户基础信息。
func (a *App) authProfile(c *gin.Context) {
	userID := parseUserID(c.GetHeader("Authorization"))
	if userID == "" {
		fail(c, 40100, "未授权或 token 非法")
		return
	}

	now := nowMs()
	ok(c, gin.H{
		"user_id":      userID,
		"username":     userID,
		"email":        "",
		"display_name": userID,
		"avatar_url":   "",
		"created_at":   now,
	})
}
