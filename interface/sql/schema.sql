CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    title VARCHAR(256) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS files (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    session_id VARCHAR(64),
    task_id VARCHAR(64),
    filename VARCHAR(256) NOT NULL,
    file_type VARCHAR(16) NOT NULL,
    file_size BIGINT NOT NULL,
    storage_url VARCHAR(1024) NOT NULL,
    object_key VARCHAR(1024) NOT NULL DEFAULT '',
    checksum VARCHAR(128) NOT NULL DEFAULT '',
    purpose VARCHAR(32) NOT NULL DEFAULT 'reference',
    created_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_files_user ON files(user_id);
CREATE INDEX IF NOT EXISTS idx_files_session ON files(session_id);
CREATE INDEX IF NOT EXISTS idx_files_task ON files(task_id);

CREATE TABLE IF NOT EXISTS file_delete_jobs (
    id VARCHAR(64) PRIMARY KEY,
    file_id VARCHAR(64) NOT NULL,
    storage_url VARCHAR(1024) NOT NULL,
    object_key VARCHAR(1024) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    retry_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_file_delete_jobs_file_id ON file_delete_jobs(file_id);
CREATE INDEX IF NOT EXISTS idx_file_delete_jobs_status ON file_delete_jobs(status);

CREATE TABLE IF NOT EXISTS search_requests (
    request_id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    query TEXT NOT NULL,
    status VARCHAR(16) NOT NULL,
    results TEXT NOT NULL DEFAULT '[]',
    summary TEXT NOT NULL DEFAULT '',
    duration BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_search_user ON search_requests(user_id);
CREATE INDEX IF NOT EXISTS idx_search_status ON search_requests(status);

ALTER TABLE files ADD COLUMN IF NOT EXISTS object_key VARCHAR(1024) NOT NULL DEFAULT '';
ALTER TABLE files ADD COLUMN IF NOT EXISTS checksum VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE file_delete_jobs ADD COLUMN IF NOT EXISTS object_key VARCHAR(1024) NOT NULL DEFAULT '';

DROP TRIGGER IF EXISTS after_files_delete_enqueue_job ON files;
DROP FUNCTION IF EXISTS trg_enqueue_file_delete_job();
