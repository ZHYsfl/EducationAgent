package server

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"multimodal-teaching-agent/oss"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type App struct {
	baseCtx          context.Context
	db               *gorm.DB
	storage          oss.Storage
	searchProvider   string
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
EXECUTE FUNCTION trg_enqueue_file_delete_job();`)

	storage, err := oss.New(oss.Config{
		Provider:   strings.ToLower(getenv("OSS_PROVIDER", "local")),
		BaseURL:    getenv("OSS_BASE_URL", "http://localhost:9500"),
		LocalPath:  getenv("OSS_LOCAL_PATH", "./storage"),
		SigningKey: getenv("OSS_SIGNING_KEY", ""),
		Bucket:     getenv("OSS_BUCKET", ""),
		Region:     getenv("OSS_REGION", ""),
		SecretID:   getenv("OSS_SECRET_ID", ""),
		SecretKey:  getenv("OSS_SECRET_KEY", ""),
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 OSS 客户端失败: %w", err)
	}

	threshold, _ := strconv.ParseFloat(getenv("KB_SCORE_THRESHOLD", "0.5"), 64)
	maxRetry, _ := strconv.Atoi(getenv("FILE_DELETE_MAX_RETRY", "5"))
	if maxRetry <= 0 {
		maxRetry = 5
	}

	return &App{
		baseCtx:          context.Background(),
		db:               db,
		storage:          storage,
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

func (a *App) Start() error {
	a.startFileDeleteWorker(a.baseCtx)
	r := setupRouter(a)
	return r.Run(":" + getenv("PORT", "9500"))
}
