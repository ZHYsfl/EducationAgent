// Package store PostgreSQL 元数据层实现 MetaStore 接口。
//
// 建表 DDL（首次运行自动执行）：
//
//	 CREATE TABLE IF NOT EXISTS kb_collections (
//	   collection_id TEXT PRIMARY KEY,
//	   user_id       TEXT NOT NULL,
//	   name          TEXT NOT NULL,
//	   subject       TEXT NOT NULL,
//	   description   TEXT,
//	   doc_count     INTEGER NOT NULL DEFAULT 0,
//	   created_at    BIGINT NOT NULL,
//	   updated_at    BIGINT NOT NULL
//	 );
//
//	 CREATE TABLE IF NOT EXISTS kb_documents (
//	   doc_id        TEXT PRIMARY KEY,
//	   collection_id TEXT NOT NULL REFERENCES kb_collections(collection_id),
//	   user_id       TEXT NOT NULL,
//	   file_id       TEXT,
//	   title         TEXT,
//	   doc_type      TEXT NOT NULL,
//	   chunk_count   INTEGER NOT NULL DEFAULT 0,
//	   status        TEXT NOT NULL DEFAULT 'pending',
//	   error_message TEXT,
//	   source_url    TEXT,
//	   created_at    BIGINT NOT NULL
//	 );
//
//	 CREATE TABLE IF NOT EXISTS kb_user_urls (
//	   user_id    TEXT NOT NULL,
//	   source_url TEXT NOT NULL,
//	   PRIMARY KEY (user_id, source_url)
//	 );
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver
	"kb-service/internal/model"
)

// PostgresStore PostgreSQL 元数据层
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 建立 PG 连接并自动迁移表结构
// dsn 示例："postgres://user:pass@localhost:5432/kbdb?sslmode=disable"
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

// migrate 自动建表（幂等）
func (s *PostgresStore) migrate(ctx context.Context) error {
	ddl := `
CREATE TABLE IF NOT EXISTS kb_collections (
    collection_id TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL,
    name          TEXT NOT NULL,
    subject       TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    doc_count     INTEGER NOT NULL DEFAULT 0,
    created_at    BIGINT NOT NULL,
    updated_at    BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kb_collections_user_id ON kb_collections(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS kb_documents (
    doc_id        TEXT PRIMARY KEY,
    collection_id TEXT NOT NULL,
    user_id       TEXT NOT NULL,
    file_id       TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL DEFAULT '',
    doc_type      TEXT NOT NULL,
    chunk_count   INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'processing',
    error_message TEXT NOT NULL DEFAULT '',
    source_url    TEXT NOT NULL DEFAULT '',
    created_at    BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kb_documents_coll ON kb_documents(collection_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_kb_documents_user ON kb_documents(user_id);

CREATE TABLE IF NOT EXISTS kb_user_urls (
    user_id    TEXT NOT NULL,
    source_url TEXT NOT NULL,
    PRIMARY KEY (user_id, source_url)
);
`
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

// ── Collections ──────────────────────────────────────────────────────────────

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

// ── Documents ─────────────────────────────────────────────────────────────────

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

	// 先查出 collection_id 和 source_url
	var collID, userID, sourceURL string
	err := s.db.QueryRowContext(ctx,
		`SELECT collection_id, user_id, source_url FROM kb_documents WHERE doc_id=$1`, docID).
		Scan(&collID, &userID, &sourceURL)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}

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

// ── URL 去重 ──────────────────────────────────────────────────────────────────

func (s *PostgresStore) URLExistsForUser(userID, sourceURL string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM kb_user_urls WHERE user_id=$1 AND source_url=$2)`,
		userID, sourceURL).Scan(&exists)
	return exists, err
}
