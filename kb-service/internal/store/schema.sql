-- kb-service 数据库表结构
-- 执行方式：psql $DATABASE_URL -f schema.sql
-- 或由 NewPostgresStore 自动加载执行（幂等）

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
