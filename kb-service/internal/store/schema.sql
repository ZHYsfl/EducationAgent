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

-- 内容指纹表：用 SHA-256(content) 去重，user_id + content_hash 唯一
-- 不同用户允许有相同内容（不跨用户去重），同一用户不能重复索引相同内容
CREATE TABLE IF NOT EXISTS kb_content_hashes (
    user_id       TEXT NOT NULL,
    content_hash  TEXT NOT NULL,          -- SHA-256(content) hex string, 64 chars
    doc_id        TEXT NOT NULL,          -- 关联的 doc_id
    created_at    BIGINT NOT NULL,
    PRIMARY KEY (user_id, content_hash)
);
CREATE INDEX IF NOT EXISTS idx_kb_content_hashes_hash ON kb_content_hashes(content_hash);

-- Dead Letter Queue：持久化索引失败任务，支持服务重启后自动重放
CREATE TABLE IF NOT EXISTS kb_dlq (
    id           BIGSERIAL PRIMARY KEY,
    doc_id       TEXT NOT NULL,
    collection_id TEXT NOT NULL,
    user_id      TEXT NOT NULL,
    file_url     TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    file_type    TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL DEFAULT '',
    retry        INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kb_dlq_created_at ON kb_dlq(created_at ASC);
