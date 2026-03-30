package server

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"multimodal-teaching-agent/oss"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type App struct {
	baseCtx          context.Context
	db               *gorm.DB
	storage          oss.Storage
	ossProvider      string
	ossBaseURL       string
	ossPublicBaseURL string
	ossBucket        string
	ossEndpoint      string
	ossUseSSL        bool
	ossAllowUnsigned bool
	searchProvider   string // legacy: single provider (kept for backward compatibility)
	searchProviders  []SearchProvider
	searchStrategy   string
	searchTimeout    time.Duration
	serpAPIKey       string
	metasoAPIKey     string
	metasoAPIURL     string
	kbDedupEnabled   bool
	kbQueryURL       string
	kbScoreThreshold float64
	workerMaxRetry   int
}

func InitApp() (*App, error) {
	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	if err = db.AutoMigrate(&SessionModel{}, &FileModel{}, &FileDeleteJobModel{}, &SearchRequestModel{}); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}
	_ = db.Exec(`ALTER TABLE files ADD COLUMN IF NOT EXISTS object_key VARCHAR(1024) NOT NULL DEFAULT ''`)
	_ = db.Exec(`ALTER TABLE files ADD COLUMN IF NOT EXISTS checksum VARCHAR(128) NOT NULL DEFAULT ''`)
	_ = db.Exec(`ALTER TABLE file_delete_jobs ADD COLUMN IF NOT EXISTS object_key VARCHAR(1024) NOT NULL DEFAULT ''`)
	_ = db.Exec(`DROP TRIGGER IF EXISTS after_files_delete_enqueue_job ON files`)
	_ = db.Exec(`DROP FUNCTION IF EXISTS trg_enqueue_file_delete_job()`)

	ossProvider := strings.ToLower(getenv("OSS_PROVIDER", "local"))
	ossBaseURL := getenv("OSS_BASE_URL", "http://localhost:9500")
	ossPublicBaseURL := getenv("OSS_PUBLIC_BASE_URL", "")
	ossBucket := getenv("OSS_BUCKET", getenv("MINIO_BUCKET", ""))
	ossEndpoint := getenv("OSS_ENDPOINT", getenv("MINIO_ENDPOINT", ""))
	ossUseSSL := strings.EqualFold(getenv("OSS_USE_SSL", getenv("MINIO_USE_SSL", "false")), "true")
	ossAllowUnsigned := strings.EqualFold(getenv("OSS_ALLOW_UNSIGNED", "false"), "true")
	storage, err := oss.New(oss.Config{
		Provider:   ossProvider,
		BaseURL:    ossBaseURL,
		LocalPath:  getenv("OSS_LOCAL_PATH", "./storage"),
		SigningKey: getenv("OSS_SIGNING_KEY", ""),
		Bucket:     ossBucket,
		Region:     getenv("OSS_REGION", ""),
		SecretID:   getenv("OSS_SECRET_ID", getenv("MINIO_ACCESS_KEY", "")),
		SecretKey:  getenv("OSS_SECRET_KEY", getenv("MINIO_SECRET_KEY", "")),
		Endpoint:   ossEndpoint,
		UseSSL:     ossUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 OSS 客户端失败: %w", err)
	}

	threshold, _ := strconv.ParseFloat(getenv("KB_SCORE_THRESHOLD", "0.5"), 64)
	maxRetry, _ := strconv.Atoi(getenv("FILE_DELETE_MAX_RETRY", "5"))
	if maxRetry <= 0 {
		maxRetry = 5
	}

	timeoutMs, _ := strconv.Atoi(getenv("SEARCH_TIMEOUT_MS", "10000"))
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	searchStrategy := strings.ToLower(getenv("SEARCH_STRATEGY", "merge"))
	if searchStrategy != "merge" && searchStrategy != "first_success" {
		searchStrategy = "merge"
	}

	legacyProvider := strings.ToLower(getenv("SEARCH_PROVIDER", "duckduckgo"))
	providers, _ := buildSearchProviders(
		getenv("SEARCH_PROVIDERS", ""),
		legacyProvider,
		os.Getenv("SERPAPI_KEY"),
		os.Getenv("METASO_API_KEY"),
		getenv("METASO_API_URL", "https://metaso.cn/api/open/search"),
	)

	return &App{
		baseCtx:          context.Background(),
		db:               db,
		storage:          storage,
		ossProvider:      ossProvider,
		ossBaseURL:       ossBaseURL,
		ossPublicBaseURL: ossPublicBaseURL,
		ossBucket:        ossBucket,
		ossEndpoint:      ossEndpoint,
		ossUseSSL:        ossUseSSL,
		ossAllowUnsigned: ossAllowUnsigned,
		searchProvider:   legacyProvider,
		searchProviders:  providers,
		searchStrategy:   searchStrategy,
		searchTimeout:    time.Duration(timeoutMs) * time.Millisecond,
		serpAPIKey:       os.Getenv("SERPAPI_KEY"),
		metasoAPIKey:     os.Getenv("METASO_API_KEY"),
		metasoAPIURL:     getenv("METASO_API_URL", "https://metaso.cn/api/open/search"),
		kbDedupEnabled:   strings.EqualFold(getenv("KB_DEDUP_ENABLED", "false"), "true"),
		kbQueryURL:       getenv("KB_QUERY_URL", "http://localhost:9200/api/v1/kb/query"),
		kbScoreThreshold: threshold,
		workerMaxRetry:   maxRetry,
	}, nil
}

func (a *App) Start() error {
	a.startFileDeleteWorker(a.baseCtx)
	r := SetupRouter(a)
	return r.Run(":" + getenv("PORT", "9500"))
}
