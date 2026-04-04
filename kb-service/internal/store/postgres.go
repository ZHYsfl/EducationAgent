// Package store PostgreSQL MetaStore 接口实现.
// DDL 由 schema.sql 提供，NewPostgresStore 启动时自动执行（幂等）.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver
	"kb-service/internal/model"
)

//go:embed schema.sql
var schemaDDL string

// PostgresStore PostgreSQL 元数据存储实现
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 创建 PostgreSQL 连接并执行 schema 迁移
// dsn 格式: postgres://user:pass@localhost:5432/kbdb?sslmode=disable
func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open pg: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("pg ping: %w", err)
	}

	s := &PostgresStore{db: db}
	if err := s.migrate(ctx); err != nil {
		return nil, fmt.Errorf("pg migrate: %w", err)
	}
	return s, nil
}

func (s *PostgresStore) Close() error { return s.db.Close() }

func (s *PostgresStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaDDL)
	return err
}

// ── Collections ─────────────────────────────────────────────────────────────────

func (s *PostgresStore) CreateCollection(c *model.KBCollection) error {
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO kb_collections
			(collection_id, user_id, name, subject, description, doc_count, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		c.CollectionID, c.UserID, c.Name, c.Subject, c.Description,
		c.DocCount, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) ListCollections(userID string) ([]model.KBCollection, int, error) {
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT collection_id, user_id, name, subject, description, doc_count, created_at, updated_at
		FROM kb_collections
		WHERE user_id = $1
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []model.KBCollection
	for rows.Next() {
		var c model.KBCollection
		if err := rows.Scan(&c.CollectionID, &c.UserID, &c.Name, &c.Subject,
			&c.Description, &c.DocCount, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, c)
	}
	return list, len(list), rows.Err()
}

func (s *PostgresStore) GetCollection(collID string) (*model.KBCollection, error) {
	var c model.KBCollection
	err := s.db.QueryRowContext(context.Background(), `
		SELECT collection_id, user_id, name, subject, description, doc_count, created_at, updated_at
		FROM kb_collections WHERE collection_id = $1`, collID).
		Scan(&c.CollectionID, &c.UserID, &c.Name, &c.Subject,
			&c.Description, &c.DocCount, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *PostgresStore) GetDefaultCollection(userID string) (*model.KBCollection, error) {
	var c model.KBCollection
	err := s.db.QueryRowContext(context.Background(), `
		SELECT collection_id, user_id, name, subject, description, doc_count, created_at, updated_at
		FROM kb_collections WHERE user_id = $1
		ORDER BY created_at ASC LIMIT 1`, userID).
		Scan(&c.CollectionID, &c.UserID, &c.Name, &c.Subject,
			&c.Description, &c.DocCount, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *PostgresStore) IncrDocCount(collID string, now int64) error {
	_, err := s.db.ExecContext(context.Background(), `
		UPDATE kb_collections
		SET doc_count = doc_count + 1, updated_at = $1
		WHERE collection_id = $2`, now, collID)
	return err
}

func (s *PostgresStore) DecrDocCount(collID string, now int64) error {
	_, err := s.db.ExecContext(context.Background(), `
		UPDATE kb_collections
		SET doc_count = GREATEST(doc_count - 1, 0), updated_at = $1
		WHERE collection_id = $2`, now, collID)
	return err
}

// ── Documents ──────────────────────────────────────────────────────────────────

func (s *PostgresStore) CreateDocument(d *model.KBDocument) error {
	return s.createDocInternal(d, "", "")
}

func (s *PostgresStore) CreateDocumentFull(d *model.KBDocument, userID, sourceURL string) error {
	return s.createDocInternal(d, userID, sourceURL)
}

func (s *PostgresStore) createDocInternal(d *model.KBDocument, userID, sourceURL string) error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.ExecContext(ctx, `
		INSERT INTO kb_documents
			(doc_id, collection_id, user_id, file_id, title, doc_type,
			 chunk_count, status, error_message, source_url, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		d.DocID, d.CollectionID, userID, d.FileID, d.Title, d.DocType,
		d.ChunkCount, d.Status, d.ErrorMessage, sourceURL, d.CreatedAt,
	)
	if err != nil {
		return err
	}

	// URL 去重记录
	if sourceURL != "" && userID != "" {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO kb_user_urls (user_id, source_url)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, sourceURL)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) GetDocument(docID string) (*model.KBDocument, error) {
	var d model.KBDocument
	err := s.db.QueryRowContext(context.Background(), `
		SELECT doc_id, collection_id, file_id, title, doc_type,
			   chunk_count, status, error_message, created_at
		FROM kb_documents WHERE doc_id = $1`, docID).
		Scan(&d.DocID, &d.CollectionID, &d.FileID, &d.Title, &d.DocType,
			&d.ChunkCount, &d.Status, &d.ErrorMessage, &d.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PostgresStore) UpdateDocumentStatus(docID, status, errMsg string, chunkCount int) error {
	_, err := s.db.ExecContext(context.Background(), `
		UPDATE kb_documents
		SET status=$1, error_message=$2, chunk_count=$3
		WHERE doc_id=$4`, status, errMsg, chunkCount, docID)
	return err
}

func (s *PostgresStore) DeleteDocument(docID string) (string, error) {
	ctx := context.Background()

	var collID, userID, sourceURL, contentHash string
	err := s.db.QueryRowContext(ctx,
		`SELECT collection_id, user_id, source_url FROM kb_documents WHERE doc_id=$1`, docID).
		Scan(&collID, &userID, &sourceURL)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// 取出内容指纹用于清理
	_ = s.db.QueryRowContext(ctx,
		`SELECT content_hash FROM kb_content_hashes WHERE doc_id=$1`, docID).Scan(&contentHash)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err = tx.ExecContext(ctx, `DELETE FROM kb_documents WHERE doc_id=$1`, docID); err != nil {
		return "", err
	}
	if sourceURL != "" && userID != "" {
		if _, err = tx.ExecContext(ctx,
			`DELETE FROM kb_user_urls WHERE user_id=$1 AND source_url=$2`, userID, sourceURL); err != nil {
			return "", err
		}
	}
	if contentHash != "" {
		if _, err = tx.ExecContext(ctx,
			`DELETE FROM kb_content_hashes WHERE user_id=$1 AND content_hash=$2`, userID, contentHash); err != nil {
			return "", err
		}
	}
	return collID, tx.Commit()
}

func (s *PostgresStore) ListDocumentsByCollection(collID string, page, pageSize int) ([]model.KBDocument, int, error) {
	ctx := context.Background()

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kb_documents WHERE collection_id=$1`, collID).
		Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx, `
		SELECT doc_id, collection_id, file_id, title, doc_type,
			   chunk_count, status, error_message, created_at
		FROM kb_documents
		WHERE collection_id=$1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, collID, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []model.KBDocument
	for rows.Next() {
		var d model.KBDocument
		if err := rows.Scan(&d.DocID, &d.CollectionID, &d.FileID, &d.Title, &d.DocType,
			&d.ChunkCount, &d.Status, &d.ErrorMessage, &d.CreatedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, d)
	}
	return list, total, rows.Err()
}

// ── URL 去重 ───────────────────────────────────────────────────────────────────

func (s *PostgresStore) URLExistsForUser(userID, sourceURL string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM kb_user_urls WHERE user_id=$1 AND source_url=$2)`,
		userID, sourceURL).Scan(&exists)
	return exists, err
}

// ── 文件去重 ───────────────────────────────────────────────────────────────────

// FileExistsForUser 检查同一用户下 file_id 是否已存在（排除 failed 状态的文档）
func (s *PostgresStore) FileExistsForUser(userID, fileID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM kb_documents WHERE user_id=$1 AND file_id=$2 AND status!='failed')`,
		userID, fileID).Scan(&exists)
	return exists, err
}

// ── 内容去重 ───────────────────────────────────────────────────────────────────

// ContentHashExists 检查同一用户下该内容指纹是否已索引
func (s *PostgresStore) ContentHashExists(userID, contentHash string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM kb_content_hashes WHERE user_id=$1 AND content_hash=$2)`,
		userID, contentHash).Scan(&exists)
	return exists, err
}

// RecordContentHash 索引成功后写入内容指纹（幂等，冲突时忽略）
func (s *PostgresStore) RecordContentHash(userID, contentHash, docID string, createdAt int64) error {
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO kb_content_hashes (user_id, content_hash, doc_id, created_at)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (user_id, content_hash) DO NOTHING`,
		userID, contentHash, docID, createdAt)
	return err
}

// ── DLQ ────────────────────────────────────────────────────────────────────────

func (s *PostgresStore) DLQPush(job store.IndexJob) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(context.Background(), `
		INSERT INTO kb_dlq
			(doc_id, collection_id, user_id, file_url, content, file_type, title, retry, last_error, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		job.DocID, job.CollectionID, job.UserID, job.FileURL, job.Content,
		job.FileType, job.Title, job.Retry, "", now, now)
	return err
}

func (s *PostgresStore) DLQPop(count int) ([]store.IndexJob, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, doc_id, collection_id, user_id, file_url, content, file_type, title, retry
		FROM kb_dlq
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, count)
	if err != nil {
		return nil, err
	}
	var jobs []store.IndexJob
	var ids []int64
	for rows.Next() {
		var j store.IndexJob
		var id int64
		if err := rows.Scan(&id, &j.DocID, &j.CollectionID, &j.UserID,
			&j.FileURL, &j.Content, &j.FileType, &j.Title, &j.Retry); err != nil {
			rows.Close()
			return nil, err
		}
		jobs = append(jobs, j)
		ids = append(ids, id)
	}
	rows.Close()
	if len(jobs) == 0 {
		return nil, nil
	}

	for _, id := range ids {
		_, err := tx.ExecContext(ctx, `DELETE FROM kb_dlq WHERE id=$1`, id)
		if err != nil {
			return nil, err
		}
	}
	return jobs, tx.Commit()
}

func (s *PostgresStore) DLQSize() (int, error) {
	var n int
	err := s.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM kb_dlq`).Scan(&n)
	return n, err
}

// ── 健康检查 ───────────────────────────────────────────────────────────────────

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
