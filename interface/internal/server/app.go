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
	schemaFile := getenv("DB_SCHEMA_FILE", "./sql/schema.sql")
	if err = applySchemaSQL(db, schemaFile); err != nil {
		return nil, fmt.Errorf("数据库迁移失败: %w", err)
	}

	ossProvider := strings.ToLower(getenv("OSS_PROVIDER", "local"))
	ossBaseURL := getenv("OSS_BASE_URL", "http://localhost:9500")
	ossPublicBaseURL := getenv("OSS_PUBLIC_BASE_URL", "")
	ossAllowUnsigned := strings.EqualFold(getenv("OSS_ALLOW_UNSIGNED", "false"), "true")
	storage, err := oss.New(oss.Config{
		Provider:   ossProvider,
		BaseURL:    ossBaseURL,
		LocalPath:  getenv("OSS_LOCAL_PATH", "./storage"),
		SigningKey: getenv("OSS_SIGNING_KEY", ""),
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

func applySchemaSQL(db *gorm.DB, schemaFile string) error {
	sqlBytes, err := os.ReadFile(schemaFile)
	if err != nil {
		return fmt.Errorf("读取 SQL 文件失败 (%s): %w", schemaFile, err)
	}
	lines := strings.Split(string(sqlBytes), "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		cleaned = append(cleaned, line)
	}
	statements := strings.Split(strings.Join(cleaned, "\n"), ";")
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("执行 SQL 失败: %w", err)
		}
	}
	return nil
}
