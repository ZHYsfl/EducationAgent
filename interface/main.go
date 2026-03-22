package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type SessionModel struct {
	ID        string `gorm:"primaryKey;column:id;size:64"`
	UserID    string `gorm:"column:user_id;size:64;not null;index:idx_sessions_user"`
	Title     string `gorm:"column:title;size:256;default:''"`
	Status    string `gorm:"column:status;size:16;default:'active'"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
	UpdatedAt int64  `gorm:"column:updated_at;not null"`
}

func (SessionModel) TableName() string { return "sessions" }

type FileModel struct {
	ID         string  `gorm:"primaryKey;column:id;size:64"`
	UserID     string  `gorm:"column:user_id;size:64;not null;index:idx_files_user"`
	SessionID  *string `gorm:"column:session_id;size:64;index:idx_files_session"`
	TaskID     *string `gorm:"column:task_id;size:64;index:idx_files_task"`
	Filename   string  `gorm:"column:filename;size:256;not null"`
	FileType   string  `gorm:"column:file_type;size:16;not null"`
	FileSize   int64   `gorm:"column:file_size;not null"`
	StorageURL string  `gorm:"column:storage_url;size:1024;not null"`
	ObjectKey  string  `gorm:"column:object_key;size:1024;not null"`
	Purpose    string  `gorm:"column:purpose;size:32;default:'reference'"`
	CreatedAt  int64   `gorm:"column:created_at;not null"`
}

func (FileModel) TableName() string { return "files" }

type FileDeleteJobModel struct {
	ID         string `gorm:"primaryKey;column:id;size:64"`
	FileID     string `gorm:"column:file_id;size:64;not null;index:idx_file_delete_jobs_file_id"`
	StorageURL string `gorm:"column:storage_url;size:1024;not null"`
	ObjectKey  string `gorm:"column:object_key;size:1024;not null"`
	Status     string `gorm:"column:status;size:16;not null;default:'pending';index:idx_file_delete_jobs_status"`
	RetryCount int    `gorm:"column:retry_count;not null;default:0"`
	LastError  string `gorm:"column:last_error;type:text;default:''"`
	CreatedAt  int64  `gorm:"column:created_at;not null"`
	UpdatedAt  int64  `gorm:"column:updated_at;not null"`
}

func (FileDeleteJobModel) TableName() string { return "file_delete_jobs" }

type SearchRequestModel struct {
	RequestID string `gorm:"primaryKey;column:request_id;size:64"`
	UserID    string `gorm:"column:user_id;size:64;not null;index:idx_search_user"`
	Query     string `gorm:"column:query;type:text;not null"`
	Status    string `gorm:"column:status;size:16;not null;index:idx_search_status"`
	Results   string `gorm:"column:results;type:text"`
	Summary   string `gorm:"column:summary;type:text"`
	Duration  int64  `gorm:"column:duration;not null;default:0"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
	UpdatedAt int64  `gorm:"column:updated_at;not null"`
}

func (SearchRequestModel) TableName() string { return "search_requests" }

type SearchResultItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source"`
}

type App struct {
	db               *gorm.DB
	minioClient      *minio.Client
	minioBucket      string
	searchProvider   string
	serpAPIKey       string
	metasoAPIKey     string
	metasoAPIURL     string
	kbDedupEnabled   bool
	kbQueryURL       string
	kbScoreThreshold float64
	workerMaxRetry   int
}

func main() {
	app, err := initApp()
	if err != nil {
		panic(err)
	}
	app.startFileDeleteWorker(context.Background())

	r := gin.Default()
	v1 := r.Group("/api/v1")
	{
		v1.POST("/files/upload", app.uploadFile)
		v1.GET("/files/:file_id", app.getFile)
		v1.DELETE("/files/:file_id", app.deleteFile)

		v1.POST("/sessions", app.createSession)
		v1.GET("/sessions/:session_id", app.getSession)
		v1.GET("/sessions", app.listSessions)
		v1.PUT("/sessions/:session_id", app.updateSession)

		v1.POST("/search/query", app.searchQuery)
		v1.GET("/search/results/:request_id", app.searchResult)
	}

	if err = r.Run(":9500"); err != nil {
		panic(err)
	}
}

func initApp() (*App, error) {
	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	if err = db.AutoMigrate(&SessionModel{}, &FileModel{}, &FileDeleteJobModel{}, &SearchRequestModel{}); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	_ = db.Exec(`ALTER TABLE files ADD COLUMN IF NOT EXISTS object_key VARCHAR(1024) NOT NULL DEFAULT ''`)
	_ = db.Exec(`ALTER TABLE file_delete_jobs ADD COLUMN IF NOT EXISTS object_key VARCHAR(1024) NOT NULL DEFAULT ''`)
	_ = db.Exec(`
CREATE OR REPLACE FUNCTION trg_enqueue_file_delete_job() RETURNS TRIGGER AS $$
DECLARE
    now_ms BIGINT;
BEGIN
    now_ms := (EXTRACT(EPOCH FROM NOW()) * 1000)::BIGINT;
    INSERT INTO file_delete_jobs (
        id, file_id, storage_url, object_key, status, retry_count, last_error, created_at, updated_at
    ) VALUES (
        'fdel_' || gen_random_uuid()::text,
        OLD.id,
        OLD.storage_url,
        COALESCE(OLD.object_key, ''),
        'pending',
        0,
        '',
        now_ms,
        now_ms
    );
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS after_files_delete_enqueue_job ON files;
CREATE TRIGGER after_files_delete_enqueue_job
AFTER DELETE ON files
FOR EACH ROW
EXECUTE FUNCTION trg_enqueue_file_delete_job();
`)

	minioEndpoint := getenv("MINIO_ENDPOINT", "127.0.0.1:9000")
	minioUser := getenv("MINIO_ACCESS_KEY", "minioadmin")
	minioPass := getenv("MINIO_SECRET_KEY", "minioadmin")
	minioUseSSL := strings.EqualFold(getenv("MINIO_USE_SSL", "false"), "true")
	minioBucket := getenv("MINIO_BUCKET", "teaching-agent")

	minioClient, err := minio.New(minioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(minioUser, minioPass, ""),
		Secure: minioUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 MinIO 客户端失败: %w", err)
	}
	ctx := context.Background()
	exists, err := minioClient.BucketExists(ctx, minioBucket)
	if err != nil {
		return nil, fmt.Errorf("检查 MinIO bucket 失败: %w", err)
	}
	if !exists {
		if err = minioClient.MakeBucket(ctx, minioBucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("创建 MinIO bucket 失败: %w", err)
		}
	}

	threshold, _ := strconv.ParseFloat(getenv("KB_SCORE_THRESHOLD", "0.5"), 64)
	maxRetry, _ := strconv.Atoi(getenv("FILE_DELETE_MAX_RETRY", "5"))
	if maxRetry <= 0 {
		maxRetry = 5
	}

	return &App{
		db:               db,
		minioClient:      minioClient,
		minioBucket:      minioBucket,
		searchProvider:   strings.ToLower(getenv("SEARCH_PROVIDER", "duckduckgo")),
		serpAPIKey:       os.Getenv("SERPAPI_KEY"),
		metasoAPIKey:     os.Getenv("METASO_API_KEY"),
		metasoAPIURL:     getenv("METASO_API_URL", "https://metaso.cn/api/open/search"),
		kbDedupEnabled:   strings.EqualFold(getenv("KB_DEDUP_ENABLED", "false"), "true"),
		kbQueryURL:       getenv("KB_QUERY_URL", "http://localhost:9200/api/v1/kb/query"),
		kbScoreThreshold: threshold,
		workerMaxRetry:   maxRetry,
	}, nil
}

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, APIResponse{Code: 200, Message: "success", Data: data})
}

func fail(c *gin.Context, code int, message string) {
	c.JSON(http.StatusOK, APIResponse{Code: code, Message: message, Data: nil})
}

func nowMs() int64 { return time.Now().UnixMilli() }
func newID(prefix string) string { return prefix + uuid.NewString() }

func (a *App) uploadFile(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		fail(c, 40001, "参数 file 不能为空")
		return
	}
	defer file.Close()

	purpose := c.PostForm("purpose")
	if purpose == "" || !isAllowedPurpose(purpose) {
		fail(c, 40001, "参数 purpose 非法")
		return
	}

	userID := parseUserID(c.GetHeader("Authorization"))
	if userID == "" {
		fail(c, 40100, "未授权或 token 非法")
		return
	}
	sessionID := emptyToNil(c.PostForm("session_id"))
	taskID := emptyToNil(c.PostForm("task_id"))

	fileID := newID("file_")
	safeName := strings.ReplaceAll(filepath.Base(header.Filename), " ", "_")
	objectKey := fmt.Sprintf("%s/%s/%s_%s", userID, purpose, fileID, safeName)

	putInfo, err := a.minioClient.PutObject(c, a.minioBucket, objectKey, file, header.Size, minio.PutObjectOptions{ContentType: header.Header.Get("Content-Type")})
	if err != nil {
		fail(c, 50000, "上传对象存储失败")
		return
	}

	storageURL := fmt.Sprintf("minio://%s/%s/%s", getenv("MINIO_ENDPOINT", "127.0.0.1:9000"), a.minioBucket, objectKey)
	rec := FileModel{
		ID:         fileID,
		UserID:     userID,
		SessionID:  sessionID,
		TaskID:     taskID,
		Filename:   header.Filename,
		FileType:   detectFileType(header.Filename),
		FileSize:   putInfo.Size,
		StorageURL: storageURL,
		ObjectKey:  objectKey,
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

	downloadURL, err := a.minioClient.PresignedGetObject(c, a.minioBucket, rec.ObjectKey, 10*time.Minute, nil)
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
		"download_url": downloadURL.String(),
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
	job := FileDeleteJobModel{
		ID:         newID("fdel_"),
		FileID:     rec.ID,
		StorageURL: rec.StorageURL,
		ObjectKey:  rec.ObjectKey,
		Status:     "pending",
		RetryCount: 0,
		LastError:  "",
		CreatedAt:  n,
		UpdatedAt:  n,
	}

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
		UserID string `json:"user_id"`
		Title  string `json:"title"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, 40001, "请求体格式错误")
		return
	}
	if req.UserID == "" {
		req.UserID = parseUserID(c.GetHeader("Authorization"))
	}
	if req.UserID == "" || !strings.HasPrefix(req.UserID, "user_") {
		fail(c, 40001, "参数 user_id 非法")
		return
	}

	n := nowMs()
	rec := SessionModel{ID: newID("sess_"), UserID: req.UserID, Title: req.Title, Status: "active", CreatedAt: n, UpdatedAt: n}
	if err := a.db.Create(&rec).Error; err != nil {
		fail(c, 50000, "创建会话失败")
		return
	}
	ok(c, gin.H{"session_id": rec.ID})
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
	ok(c, gin.H{"sessions": items, "total": total, "page": page, "page_size": pageSize})
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

	if err := a.db.Model(&SessionModel{}).Where("id = ?", sid).Updates(map[string]interface{}{
		"title":      current.Title,
		"status":     current.Status,
		"updated_at": current.UpdatedAt,
	}).Error; err != nil {
		fail(c, 50000, "更新会话失败")
		return
	}

	ok(c, gin.H{"session_id": sid, "title": current.Title, "status": current.Status, "updated_at": current.UpdatedAt})
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
	if req.UserID == "" || req.Query == "" || !strings.HasPrefix(req.UserID, "user_") {
		fail(c, 40001, "参数 user_id 或 query 非法")
		return
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 5
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
	if req.SearchType == "" {
		req.SearchType = "general"
	}

	n := nowMs()
	rec := SearchRequestModel{RequestID: req.RequestID, UserID: req.UserID, Query: req.Query, Status: "pending", Duration: 0, CreatedAt: n, UpdatedAt: n}
	if err := a.db.Create(&rec).Error; err != nil {
		fail(c, 50000, "创建搜索请求失败")
		return
	}

	go a.runSearchAsync(req.RequestID, req.UserID, req.Query, req.MaxResults, req.Language, req.SearchType)
	ok(c, gin.H{"request_id": req.RequestID, "status": "pending"})
}

func (a *App) searchResult(c *gin.Context) {
	requestID := c.Param("request_id")
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
	ok(c, gin.H{"request_id": rec.RequestID, "status": rec.Status, "results": results, "summary": rec.Summary, "duration": rec.Duration})
}

func (a *App) runSearchAsync(requestID, userID, query string, maxResults int, language, searchType string) {
	start := time.Now()

	if a.kbDedupEnabled {
		hit, err := a.kbLikelyHasAnswer(userID, query)
		if err == nil && hit {
			a.finishSearch(requestID, "completed", nil, "知识库已有高相关内容，本次不重复回注搜索结果。", time.Since(start).Milliseconds(), "")
			return
		}
	}

	results, summary, err := a.fetchSearchResults(query, maxResults, language, searchType)
	if err != nil {
		a.finishSearch(requestID, "failed", nil, "", time.Since(start).Milliseconds(), err.Error())
		return
	}
	a.finishSearch(requestID, "completed", results, summary, time.Since(start).Milliseconds(), "")
}

func (a *App) finishSearch(requestID, status string, results []SearchResultItem, summary string, duration int64, lastErr string) {
	resultsJSON := ""
	if len(results) > 0 {
		b, _ := json.Marshal(results)
		resultsJSON = string(b)
	}
	updates := map[string]interface{}{"status": status, "results": resultsJSON, "summary": summary, "duration": duration, "updated_at": nowMs()}
	if lastErr != "" {
		updates["summary"] = lastErr
	}
	_ = a.db.Model(&SearchRequestModel{}).Where("request_id = ?", requestID).Updates(updates).Error
}

func (a *App) fetchSearchResults(query string, maxResults int, language, searchType string) ([]SearchResultItem, string, error) {
	switch a.searchProvider {
	case "serpapi":
		return a.searchBySerpAPI(query, maxResults, language, searchType)
	case "metaso":
		return a.searchByMetaso(query, maxResults, searchType)
	default:
		return a.searchByDuckDuckGo(query, maxResults, searchType)
	}
}

func (a *App) searchBySerpAPI(query string, maxResults int, language, searchType string) ([]SearchResultItem, string, error) {
	if a.serpAPIKey == "" {
		return nil, "", errors.New("SERPAPI_KEY 未配置")
	}
	u := "https://serpapi.com/search.json?q=" + url.QueryEscape(query) + "&api_key=" + url.QueryEscape(a.serpAPIKey) + "&hl=" + url.QueryEscape(language)
	if searchType == "news" {
		u += "&tbm=nws"
	}
	resp, err := http.Get(u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("serpapi 返回状态码 %d", resp.StatusCode)
	}
	var raw map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, "", err
	}
	items := make([]SearchResultItem, 0, maxResults)
	if arr, ok := raw["organic_results"].([]interface{}); ok {
		for _, v := range arr {
			if len(items) >= maxResults {
				break
			}
			m, _ := v.(map[string]interface{})
			items = append(items, SearchResultItem{
				Title:   toString(m["title"]),
				URL:     toString(m["link"]),
				Snippet: refineSnippet(toString(m["snippet"]), query),
				Source:  hostOf(toString(m["link"])),
			})
		}
	}
	if len(items) == 0 {
		return nil, "", errors.New("未检索到有效结果")
	}
	return items, buildSummary(query, items), nil
}

func (a *App) searchByDuckDuckGo(query string, maxResults int, searchType string) ([]SearchResultItem, string, error) {
	q := query
	if searchType == "news" {
		q = query + " 最新 新闻"
	} else if searchType == "academic" {
		q = query + " 学术 论文"
	}
	u := "https://api.duckduckgo.com/?q=" + url.QueryEscape(q) + "&format=json&no_html=1&skip_disambig=1"
	resp, err := http.Get(u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("duckduckgo 返回状态码 %d", resp.StatusCode)
	}
	var raw struct {
		AbstractText  string `json:"AbstractText"`
		AbstractURL   string `json:"AbstractURL"`
		Heading       string `json:"Heading"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, "", err
	}
	items := make([]SearchResultItem, 0, maxResults)
	if raw.AbstractText != "" {
		items = append(items, SearchResultItem{Title: raw.Heading, URL: raw.AbstractURL, Snippet: refineSnippet(raw.AbstractText, query), Source: hostOf(raw.AbstractURL)})
	}
	for _, t := range raw.RelatedTopics {
		if len(items) >= maxResults {
			break
		}
		if t.Text == "" {
			continue
		}
		items = append(items, SearchResultItem{Title: titleFromSnippet(t.Text), URL: t.FirstURL, Snippet: refineSnippet(t.Text, query), Source: hostOf(t.FirstURL)})
	}
	if len(items) == 0 {
		return nil, "", errors.New("未检索到有效结果")
	}
	return items, buildSummary(query, items), nil
}

func (a *App) searchByMetaso(query string, maxResults int, searchType string) ([]SearchResultItem, string, error) {
	if a.metasoAPIKey == "" {
		return nil, "", errors.New("METASO_API_KEY 未配置")
	}

	metaType := "web"
	if searchType == "news" {
		metaType = "news"
	} else if searchType == "academic" {
		metaType = "academic"
	}

	body := map[string]interface{}{
		"query":       query,
		"count":       maxResults,
		"search_type": metaType,
	}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequest(http.MethodPost, a.metasoAPIURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.metasoAPIKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		rawErr, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("metaso 返回状态码 %d: %s", resp.StatusCode, string(rawErr))
	}

	var raw map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, "", err
	}

	items := make([]SearchResultItem, 0, maxResults)
	for _, key := range []string{"results", "data", "items"} {
		arr, ok := raw[key].([]interface{})
		if !ok {
			continue
		}
		for _, v := range arr {
			if len(items) >= maxResults {
				break
			}
			m, _ := v.(map[string]interface{})
			title := toString(m["title"])
			link := toString(m["url"])
			if link == "" {
				link = toString(m["link"])
			}
			snippet := toString(m["snippet"])
			if snippet == "" {
				snippet = toString(m["summary"])
			}
			source := toString(m["source"])
			if source == "" {
				source = hostOf(link)
			}
			if title == "" && snippet == "" {
				continue
			}
			items = append(items, SearchResultItem{
				Title:   titleOrDefault(title, snippet),
				URL:     link,
				Snippet: refineSnippet(snippet, query),
				Source:  source,
			})
		}
		if len(items) > 0 {
			break
		}
	}

	if len(items) == 0 {
		return nil, "", errors.New("秘塔 API 未返回可用结果")
	}

	return items, buildSummary(query, items), nil
}

func (a *App) kbLikelyHasAnswer(userID, query string) (bool, error) {
	body := map[string]interface{}{"user_id": userID, "query": query, "top_k": 1, "score_threshold": a.kbScoreThreshold}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, a.kbQueryURL, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return false, fmt.Errorf("kb query status=%d", resp.StatusCode)
	}
	var raw struct {
		Code int `json:"code"`
		Data struct {
			Chunks []struct {
				Score float64 `json:"score"`
			} `json:"chunks"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return false, err
	}
	if raw.Code != 200 || len(raw.Data.Chunks) == 0 {
		return false, nil
	}
	return raw.Data.Chunks[0].Score >= a.kbScoreThreshold, nil
}

func (a *App) startFileDeleteWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				a.pollAndProcessDeleteJobs(ctx)
			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}()
}

func (a *App) pollAndProcessDeleteJobs(ctx context.Context) {
	var jobs []FileDeleteJobModel
	if err := a.db.Where("status IN ? AND retry_count < ?", []string{"pending", "failed"}, a.workerMaxRetry).Order("updated_at ASC").Limit(20).Find(&jobs).Error; err != nil {
		return
	}
	for _, job := range jobs {
		a.processFileDeleteJob(ctx, job)
	}
}

func (a *App) processFileDeleteJob(ctx context.Context, job FileDeleteJobModel) {
	_ = a.db.Model(&FileDeleteJobModel{}).Where("id = ?", job.ID).Updates(map[string]interface{}{"status": "processing", "updated_at": nowMs()}).Error

	err := a.minioClient.RemoveObject(ctx, a.minioBucket, job.ObjectKey, minio.RemoveObjectOptions{})
	if err != nil {
		updates := map[string]interface{}{"retry_count": gorm.Expr("retry_count + 1"), "last_error": err.Error(), "updated_at": nowMs(), "status": "failed"}
		_ = a.db.Model(&FileDeleteJobModel{}).Where("id = ?", job.ID).Updates(updates).Error
		return
	}
	_ = a.db.Model(&FileDeleteJobModel{}).Where("id = ?", job.ID).Updates(map[string]interface{}{"status": "done", "last_error": "", "updated_at": nowMs()}).Error
}

func isAllowedPurpose(v string) bool {
	switch v {
	case "reference", "export", "knowledge_base", "render":
		return true
	default:
		return false
	}
}

func isAllowedSessionStatus(v string) bool {
	switch v {
	case "active", "completed", "archived":
		return true
	default:
		return false
	}
}

func detectFileType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pdf":
		return "pdf"
	case ".docx":
		return "docx"
	case ".pptx":
		return "pptx"
	case ".jpg", ".jpeg", ".png", ".webp":
		return "image"
	case ".mp4", ".webm":
		return "video"
	case ".html", ".htm":
		return "html"
	default:
		return "other"
	}
}

func parsePositiveInt(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func parseUserID(auth string) string {
	if strings.HasPrefix(auth, "Bearer ") {
		v := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if strings.HasPrefix(v, "user_") {
			return v
		}
	}
	return ""
}

func emptyToNil(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func getenv(k, d string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d
	}
	return v
}

func toString(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func titleFromSnippet(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "搜索结果"
	}
	if len([]rune(s)) > 20 {
		r := []rune(s)
		return string(r[:20]) + "..."
	}
	return s
}

func titleOrDefault(title, snippet string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	return titleFromSnippet(snippet)
}

func refineSnippet(snippet, query string) string {
	s := strings.TrimSpace(snippet)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "<", "")
	s = strings.ReplaceAll(s, ">", "")
	if s == "" {
		return fmt.Sprintf("与“%s”相关的检索结果摘要。", query)
	}
	if len([]rune(s)) > 180 {
		r := []rune(s)
		s = string(r[:180]) + "..."
	}
	return s
}

func buildSummary(query string, items []SearchResultItem) string {
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf("围绕“%s”检索到 %d 条结果，已完成摘要精炼，可直接用于回注与知识库沉淀。", query, len(items))
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	return u.Host
}
