CREATE TABLE IF NOT EXISTS memory_entries (
    id                VARCHAR(64) PRIMARY KEY,
    user_id           VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category          VARCHAR(16) NOT NULL,
    key               VARCHAR(128) NOT NULL,
    value             TEXT NOT NULL,
    context           VARCHAR(128) DEFAULT 'general',
    confidence        REAL DEFAULT 1.0,
    source            VARCHAR(16) DEFAULT 'explicit',
    source_session_id VARCHAR(64) REFERENCES sessions(id) ON DELETE SET NULL,
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_memory_user ON memory_entries(user_id);
CREATE INDEX IF NOT EXISTS idx_memory_user_category ON memory_entries(user_id, category);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_user_key_context ON memory_entries(user_id, key, context);
