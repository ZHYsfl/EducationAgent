BEGIN;

-- ============================================================
-- Agent Project PostgreSQL DDL
-- Source baseline: system_interface_spec.md
-- Notes:
-- 1. This script keeps the official Chapter 7 tables/fields aligned.
-- 2. It adds missing persistence tables that are required by Chapters 2/3/4/5/7/8.
-- 3. All timestamps use Unix milliseconds (BIGINT).
-- 4. JSON arrays / flexible structs use JSONB.
-- Target: PostgreSQL 15+
-- ============================================================

-- Optional extension for future UUID/text utilities.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ============================================================
-- Helper trigger: maintain updated_at in Unix milliseconds
-- ============================================================
CREATE OR REPLACE FUNCTION set_updated_at_unix_ms()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at := (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ============================================================
-- 1. users
-- Spec base: Chapter 7.2.1
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
    id              VARCHAR(64) PRIMARY KEY,
    username        VARCHAR(64)  NOT NULL UNIQUE,
    email           VARCHAR(128) NOT NULL UNIQUE,
    password_hash   VARCHAR(256) NOT NULL,
    display_name    VARCHAR(128) NOT NULL DEFAULT '',
    subject         VARCHAR(64)  NOT NULL DEFAULT '',
    school          VARCHAR(128) NOT NULL DEFAULT '',
    role            VARCHAR(16)  NOT NULL DEFAULT 'teacher',
    created_at      BIGINT       NOT NULL,
    updated_at      BIGINT       NOT NULL,
    CONSTRAINT chk_users_role CHECK (role IN ('teacher', 'admin'))
);

-- ============================================================
-- 2. pending_registrations
-- Spec base: Chapter 7.2.1A
-- ============================================================
CREATE TABLE IF NOT EXISTS pending_registrations (
    user_id                     VARCHAR(64)  PRIMARY KEY,
    username                    VARCHAR(64)  NOT NULL UNIQUE,
    email                       VARCHAR(128) NOT NULL UNIQUE,
    password_hash               VARCHAR(256) NOT NULL,
    display_name                VARCHAR(128) NOT NULL DEFAULT '',
    subject                     VARCHAR(64)  NOT NULL DEFAULT '',
    school                      VARCHAR(128) NOT NULL DEFAULT '',
    role                        VARCHAR(16)  NOT NULL DEFAULT 'teacher',
    verification_token_hash     VARCHAR(128) NOT NULL UNIQUE,
    verification_expires_at     BIGINT       NOT NULL,
    verification_sent_at        BIGINT       NOT NULL,
    created_at                  BIGINT       NOT NULL,
    updated_at                  BIGINT       NOT NULL,
    CONSTRAINT chk_pending_regs_role CHECK (role IN ('teacher', 'admin'))
);
CREATE INDEX IF NOT EXISTS idx_pending_regs_expires_at
    ON pending_registrations(verification_expires_at);

-- ============================================================
-- 3. sessions
-- Spec base: Chapter 7.2.2
-- Added fields:
--   active_task_id / pending_questions for Session extension in 0.5
-- ============================================================
CREATE TABLE IF NOT EXISTS sessions (
    id                  VARCHAR(64) PRIMARY KEY,
    user_id             VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title               VARCHAR(256) NOT NULL DEFAULT '',
    status              VARCHAR(16) NOT NULL DEFAULT 'active',
    active_task_id      VARCHAR(64),
    pending_questions   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          BIGINT NOT NULL,
    updated_at          BIGINT NOT NULL,
    CONSTRAINT chk_sessions_status CHECK (status IN ('active', 'completed', 'archived'))
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_active_task ON sessions(active_task_id);

-- ============================================================
-- 4. tasks
-- Spec base: Chapter 7.2.3 / 3.2
-- ============================================================
CREATE TABLE IF NOT EXISTS tasks (
    id              VARCHAR(64) PRIMARY KEY,
    session_id      VARCHAR(64) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id         VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    topic           VARCHAR(256) NOT NULL,
    description     TEXT         NOT NULL DEFAULT '',
    total_pages     INT          NOT NULL DEFAULT 0,
    audience        VARCHAR(128) NOT NULL DEFAULT '',
    global_style    VARCHAR(128) NOT NULL DEFAULT '',
    status          VARCHAR(16)  NOT NULL DEFAULT 'pending',
    created_at      BIGINT       NOT NULL,
    updated_at      BIGINT       NOT NULL,
    CONSTRAINT chk_tasks_status CHECK (status IN ('pending', 'generating', 'completed', 'failed', 'exporting')),
    CONSTRAINT chk_tasks_total_pages CHECK (total_pages >= 0)
);
CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_user ON tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);

ALTER TABLE sessions
    ADD CONSTRAINT fk_sessions_active_task
    FOREIGN KEY (active_task_id) REFERENCES tasks(id) ON DELETE SET NULL;

-- ============================================================
-- 5. files
-- Spec base: Chapter 7.2.4 / 7.3.4
-- ============================================================
CREATE TABLE IF NOT EXISTS files (
    id              VARCHAR(64)   PRIMARY KEY,
    user_id         VARCHAR(64)   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id      VARCHAR(64)   REFERENCES sessions(id) ON DELETE SET NULL,
    task_id         VARCHAR(64)   REFERENCES tasks(id) ON DELETE SET NULL,
    filename        VARCHAR(256)  NOT NULL,
    file_type       VARCHAR(16)   NOT NULL,
    file_size       BIGINT        NOT NULL,
    storage_url     VARCHAR(1024) NOT NULL,
    purpose         VARCHAR(32)   NOT NULL DEFAULT 'reference',
    created_at      BIGINT        NOT NULL,
    CONSTRAINT chk_files_file_type CHECK (file_type IN ('pdf', 'docx', 'pptx', 'image', 'video', 'html', 'text')),
    CONSTRAINT chk_files_purpose CHECK (purpose IN ('reference', 'export', 'knowledge_base', 'render')),
    CONSTRAINT chk_files_file_size CHECK (file_size >= 0)
);
CREATE INDEX IF NOT EXISTS idx_files_user ON files(user_id);
CREATE INDEX IF NOT EXISTS idx_files_task ON files(task_id);
CREATE INDEX IF NOT EXISTS idx_files_session ON files(session_id);
CREATE INDEX IF NOT EXISTS idx_files_purpose ON files(purpose);

-- ============================================================
-- 6. kb_collections
-- Spec base: Chapter 4.2.1 (missing in Chapter 7 DDL, added here)
-- ============================================================
CREATE TABLE IF NOT EXISTS kb_collections (
    id              VARCHAR(64) PRIMARY KEY,
    user_id         VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            VARCHAR(128) NOT NULL,
    subject         VARCHAR(64)  NOT NULL DEFAULT '',
    description     TEXT         NOT NULL DEFAULT '',
    doc_count       INT          NOT NULL DEFAULT 0,
    created_at      BIGINT       NOT NULL,
    updated_at      BIGINT       NOT NULL,
    CONSTRAINT chk_kb_collections_doc_count CHECK (doc_count >= 0)
);
CREATE INDEX IF NOT EXISTS idx_kb_collections_user ON kb_collections(user_id);
CREATE INDEX IF NOT EXISTS idx_kb_collections_user_subject ON kb_collections(user_id, subject);
CREATE UNIQUE INDEX IF NOT EXISTS uq_kb_collections_user_name ON kb_collections(user_id, name);

-- ============================================================
-- 7. kb_documents
-- Spec base: Chapter 7.2.5 / 4.2.2 / 6.6
-- Added updated_at and source fields for better lifecycle tracking.
-- ============================================================
CREATE TABLE IF NOT EXISTS kb_documents (
    id              VARCHAR(64) PRIMARY KEY,
    collection_id   VARCHAR(64) NOT NULL REFERENCES kb_collections(id) ON DELETE CASCADE,
    file_id         VARCHAR(64) REFERENCES files(id) ON DELETE SET NULL,
    user_id         VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           VARCHAR(256) NOT NULL,
    doc_type        VARCHAR(16) NOT NULL,
    chunk_count     INT         NOT NULL DEFAULT 0,
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    origin          VARCHAR(32) NOT NULL DEFAULT 'manual_upload',
    source_url      VARCHAR(1024) NOT NULL DEFAULT '',
    error_message   TEXT        NOT NULL DEFAULT '',
    created_at      BIGINT      NOT NULL,
    updated_at      BIGINT      NOT NULL,
    CONSTRAINT chk_kb_documents_doc_type CHECK (doc_type IN ('pdf', 'docx', 'pptx', 'image', 'video', 'text', 'web_snippet')),
    CONSTRAINT chk_kb_documents_status CHECK (status IN ('pending', 'processing', 'indexed', 'failed')),
    CONSTRAINT chk_kb_documents_origin CHECK (origin IN ('manual_upload', 'web_search', 'system_generated')),
    CONSTRAINT chk_kb_documents_chunk_count CHECK (chunk_count >= 0)
);
CREATE INDEX IF NOT EXISTS idx_kb_docs_collection ON kb_documents(collection_id);
CREATE INDEX IF NOT EXISTS idx_kb_docs_user ON kb_documents(user_id);
CREATE INDEX IF NOT EXISTS idx_kb_docs_status ON kb_documents(status);
CREATE INDEX IF NOT EXISTS idx_kb_docs_origin ON kb_documents(origin);
CREATE UNIQUE INDEX IF NOT EXISTS uq_kb_docs_source_url_per_user
    ON kb_documents(user_id, source_url)
    WHERE source_url <> '';

-- ============================================================
-- 8. kb_text_chunks
-- Spec base: Chapter 4.2.3 / 8.1 / 8.4
-- ============================================================
CREATE TABLE IF NOT EXISTS kb_text_chunks (
    id              VARCHAR(64) PRIMARY KEY,
    doc_id          VARCHAR(64) NOT NULL REFERENCES kb_documents(id) ON DELETE CASCADE,
    content         TEXT        NOT NULL,
    page_number     INT,
    section_title   VARCHAR(256) NOT NULL DEFAULT '',
    chunk_index     INT         NOT NULL,
    start_char      INT         NOT NULL,
    end_char        INT         NOT NULL,
    image_url       VARCHAR(1024) NOT NULL DEFAULT '',
    source_type     VARCHAR(32) NOT NULL,
    created_at      BIGINT      NOT NULL,
    CONSTRAINT chk_kb_text_chunks_chunk_index CHECK (chunk_index >= 0),
    CONSTRAINT chk_kb_text_chunks_char_range CHECK (start_char >= 0 AND end_char >= start_char),
    CONSTRAINT chk_kb_text_chunks_source_type CHECK (source_type IN ('text', 'ocr', 'video_transcript'))
);
CREATE INDEX IF NOT EXISTS idx_kb_text_chunks_doc ON kb_text_chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_kb_text_chunks_doc_chunk_index ON kb_text_chunks(doc_id, chunk_index);
CREATE INDEX IF NOT EXISTS idx_kb_text_chunks_page_number ON kb_text_chunks(page_number);
CREATE UNIQUE INDEX IF NOT EXISTS uq_kb_text_chunks_doc_chunk_index ON kb_text_chunks(doc_id, chunk_index);

-- ============================================================
-- 9. task_requirements
-- Spec base: Chapter 2.4.1 / 2.4.5
-- ============================================================
CREATE TABLE IF NOT EXISTS task_requirements (
    id                  VARCHAR(64) PRIMARY KEY,
    session_id          VARCHAR(64) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id             VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_id             VARCHAR(64) REFERENCES tasks(id) ON DELETE SET NULL,
    topic               VARCHAR(256) NOT NULL DEFAULT '',
    knowledge_points    JSONB       NOT NULL DEFAULT '[]'::jsonb,
    teaching_goals      JSONB       NOT NULL DEFAULT '[]'::jsonb,
    teaching_logic      TEXT        NOT NULL DEFAULT '',
    target_audience     VARCHAR(256) NOT NULL DEFAULT '',
    key_difficulties    JSONB       NOT NULL DEFAULT '[]'::jsonb,
    duration            VARCHAR(64) NOT NULL DEFAULT '',
    total_pages         INT         NOT NULL DEFAULT 0,
    global_style        VARCHAR(128) NOT NULL DEFAULT '',
    interaction_design  TEXT        NOT NULL DEFAULT '',
    output_formats      JSONB       NOT NULL DEFAULT '[]'::jsonb,
    additional_notes    TEXT        NOT NULL DEFAULT '',
    collected_fields    JSONB       NOT NULL DEFAULT '[]'::jsonb,
    status              VARCHAR(32) NOT NULL DEFAULT 'collecting',
    created_at          BIGINT      NOT NULL,
    updated_at          BIGINT      NOT NULL,
    CONSTRAINT chk_task_requirements_status CHECK (status IN ('collecting', 'confirming', 'confirmed', 'generating')),
    CONSTRAINT chk_task_requirements_total_pages CHECK (total_pages >= 0)
);
CREATE INDEX IF NOT EXISTS idx_task_requirements_session ON task_requirements(session_id);
CREATE INDEX IF NOT EXISTS idx_task_requirements_user ON task_requirements(user_id);
CREATE INDEX IF NOT EXISTS idx_task_requirements_task ON task_requirements(task_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_task_requirements_session ON task_requirements(session_id);

-- ============================================================
-- 10. task_pages
-- Spec base: Chapter 2.3 / 3.5 / 3.7 / 3.9
-- ============================================================
CREATE TABLE IF NOT EXISTS task_pages (
    id                  VARCHAR(64) PRIMARY KEY,
    task_id             VARCHAR(64) NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    page_index          INT         NOT NULL,
    status              VARCHAR(32) NOT NULL DEFAULT 'rendering',
    render_url          VARCHAR(1024) NOT NULL DEFAULT '',
    py_code             TEXT        NOT NULL DEFAULT '',
    version             INT         NOT NULL DEFAULT 1,
    last_update         BIGINT      NOT NULL,
    created_at          BIGINT      NOT NULL,
    updated_at          BIGINT      NOT NULL,
    CONSTRAINT chk_task_pages_status CHECK (status IN ('rendering', 'completed', 'failed', 'suspended_for_human')),
    CONSTRAINT chk_task_pages_page_index CHECK (page_index >= 0),
    CONSTRAINT chk_task_pages_version CHECK (version >= 1)
);
CREATE INDEX IF NOT EXISTS idx_task_pages_task ON task_pages(task_id);
CREATE INDEX IF NOT EXISTS idx_task_pages_task_status ON task_pages(task_id, status);
CREATE INDEX IF NOT EXISTS idx_task_pages_task_page_index ON task_pages(task_id, page_index);
CREATE UNIQUE INDEX IF NOT EXISTS uq_task_pages_task_page_index ON task_pages(task_id, page_index);

-- ============================================================
-- 11. task_page_order
-- Spec base: Chapter 2.3 / 3.5 / 3.1.2 page_order
-- ============================================================
CREATE TABLE IF NOT EXISTS task_page_order (
    task_id          VARCHAR(64) NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    page_id          VARCHAR(64) NOT NULL REFERENCES task_pages(id) ON DELETE CASCADE,
    sort_order       INT         NOT NULL,
    created_at       BIGINT      NOT NULL,
    updated_at       BIGINT      NOT NULL,
    PRIMARY KEY (task_id, page_id),
    CONSTRAINT chk_task_page_order_sort_order CHECK (sort_order >= 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_task_page_order_task_sort_order ON task_page_order(task_id, sort_order);
CREATE INDEX IF NOT EXISTS idx_task_page_order_page_id ON task_page_order(page_id);

-- ============================================================
-- 12. task_reference_files
-- Spec base: Chapter 2.4.1 / 3.2 ReferenceFileReq / ReferenceFile
-- ============================================================
CREATE TABLE IF NOT EXISTS task_reference_files (
    id              BIGSERIAL PRIMARY KEY,
    requirement_id  VARCHAR(64) REFERENCES task_requirements(id) ON DELETE CASCADE,
    task_id         VARCHAR(64) REFERENCES tasks(id) ON DELETE CASCADE,
    session_id      VARCHAR(64) REFERENCES sessions(id) ON DELETE CASCADE,
    file_id         VARCHAR(64) NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    file_url        VARCHAR(1024) NOT NULL DEFAULT '',
    file_type       VARCHAR(16) NOT NULL,
    instruction     TEXT        NOT NULL DEFAULT '',
    created_at      BIGINT      NOT NULL,
    updated_at      BIGINT      NOT NULL,
    CONSTRAINT chk_task_reference_files_file_type CHECK (file_type IN ('pdf', 'docx', 'pptx', 'image', 'video', 'text')),
    CONSTRAINT chk_task_reference_files_scope CHECK (
        requirement_id IS NOT NULL OR task_id IS NOT NULL OR session_id IS NOT NULL
    )
);
CREATE INDEX IF NOT EXISTS idx_task_reference_files_task ON task_reference_files(task_id);
CREATE INDEX IF NOT EXISTS idx_task_reference_files_session ON task_reference_files(session_id);
CREATE INDEX IF NOT EXISTS idx_task_reference_files_requirement ON task_reference_files(requirement_id);
CREATE INDEX IF NOT EXISTS idx_task_reference_files_file ON task_reference_files(file_id);

-- ============================================================
-- 13. task_exports
-- Spec base: Chapter 3.6 export_id/status/download_url/format/file_size
-- ============================================================
CREATE TABLE IF NOT EXISTS task_exports (
    id                  VARCHAR(64) PRIMARY KEY,
    task_id             VARCHAR(64) NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    file_id             VARCHAR(64) REFERENCES files(id) ON DELETE SET NULL,
    format              VARCHAR(16) NOT NULL,
    status              VARCHAR(16) NOT NULL DEFAULT 'generating',
    download_url        VARCHAR(1024) NOT NULL DEFAULT '',
    file_size           BIGINT      NOT NULL DEFAULT 0,
    estimated_seconds   INT         NOT NULL DEFAULT 0,
    error_message       TEXT        NOT NULL DEFAULT '',
    created_at          BIGINT      NOT NULL,
    updated_at          BIGINT      NOT NULL,
    completed_at        BIGINT,
    CONSTRAINT chk_task_exports_format CHECK (format IN ('pptx', 'docx', 'html5')),
    CONSTRAINT chk_task_exports_status CHECK (status IN ('generating', 'completed', 'failed')),
    CONSTRAINT chk_task_exports_file_size CHECK (file_size >= 0),
    CONSTRAINT chk_task_exports_estimated_seconds CHECK (estimated_seconds >= 0)
);
CREATE INDEX IF NOT EXISTS idx_task_exports_task ON task_exports(task_id);
CREATE INDEX IF NOT EXISTS idx_task_exports_status ON task_exports(status);
CREATE INDEX IF NOT EXISTS idx_task_exports_file ON task_exports(file_id);

-- ============================================================
-- 14. suspended_pages
-- Spec base: Chapter 3.9.1 SuspendedPage
-- ============================================================
CREATE TABLE IF NOT EXISTS suspended_pages (
    page_id              VARCHAR(64) PRIMARY KEY REFERENCES task_pages(id) ON DELETE CASCADE,
    task_id              VARCHAR(64) NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    context_id           VARCHAR(64) NOT NULL,
    reason               TEXT        NOT NULL,
    suspended_at         BIGINT      NOT NULL,
    last_asked_at        BIGINT      NOT NULL,
    ask_count            INT         NOT NULL DEFAULT 0,
    pending_feedbacks    JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at           BIGINT      NOT NULL,
    updated_at           BIGINT      NOT NULL,
    CONSTRAINT chk_suspended_pages_ask_count CHECK (ask_count >= 0)
);
CREATE INDEX IF NOT EXISTS idx_suspended_pages_task ON suspended_pages(task_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_suspended_pages_context_id ON suspended_pages(context_id);

-- ============================================================
-- 15. page_merge_states
-- Spec base: Chapter 3.9.2 PageMergeState
-- ============================================================
CREATE TABLE IF NOT EXISTS page_merge_states (
    page_id              VARCHAR(64) PRIMARY KEY REFERENCES task_pages(id) ON DELETE CASCADE,
    task_id              VARCHAR(64) NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    is_running           BOOLEAN     NOT NULL DEFAULT FALSE,
    pending_feedbacks    JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at           BIGINT      NOT NULL,
    updated_at           BIGINT      NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_page_merge_states_task ON page_merge_states(task_id);
CREATE INDEX IF NOT EXISTS idx_page_merge_states_running ON page_merge_states(is_running);

-- ============================================================
-- 16. memory_objects
-- Spec base: Chapter 7.2.6C / 5.2.4 / 5.2.5
-- Must exist before memory_cards/memory_dialogue_chunks FKs in clean installs,
-- so create here and keep later ALTER-safe for existing systems.
-- ============================================================
CREATE TABLE IF NOT EXISTS memory_objects (
    id                  VARCHAR(64) PRIMARY KEY,
    user_id             VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    object_type         VARCHAR(32) NOT NULL,
    storage_key         VARCHAR(512) NOT NULL,
    storage_url         VARCHAR(1024) NOT NULL DEFAULT '',
    size_bytes          BIGINT      NOT NULL DEFAULT 0,
    checksum            VARCHAR(128) NOT NULL DEFAULT '',
    compression         VARCHAR(32) NOT NULL DEFAULT '',
    source_session_id   VARCHAR(64) REFERENCES sessions(id) ON DELETE SET NULL,
    created_at          BIGINT      NOT NULL,
    updated_at          BIGINT      NOT NULL,
    CONSTRAINT chk_memory_objects_type CHECK (object_type IN ('dialogue_chunk', 'card_snapshot', 'evidence_bundle', 'other')),
    CONSTRAINT chk_memory_objects_compression CHECK (compression IN ('', 'gzip', 'zstd', 'none')),
    CONSTRAINT chk_memory_objects_size_bytes CHECK (size_bytes >= 0)
);
CREATE INDEX IF NOT EXISTS idx_memory_objects_user ON memory_objects(user_id);
CREATE INDEX IF NOT EXISTS idx_memory_objects_type ON memory_objects(object_type);
CREATE INDEX IF NOT EXISTS idx_memory_objects_session ON memory_objects(source_session_id);

-- ============================================================
-- 17. memory_entries
-- Spec base: Chapter 7.2.6 / 5.2.2
-- ============================================================
CREATE TABLE IF NOT EXISTS memory_entries (
    id                  VARCHAR(64) PRIMARY KEY,
    user_id             VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category            VARCHAR(16) NOT NULL,
    key                 VARCHAR(128) NOT NULL,
    value               TEXT        NOT NULL,
    context             VARCHAR(128) NOT NULL DEFAULT 'general',
    confidence          REAL        NOT NULL DEFAULT 1.0,
    source              VARCHAR(16) NOT NULL DEFAULT 'explicit',
    source_session_id   VARCHAR(64) REFERENCES sessions(id) ON DELETE SET NULL,
    created_at          BIGINT      NOT NULL,
    updated_at          BIGINT      NOT NULL,
    CONSTRAINT chk_memory_entries_category CHECK (category IN ('fact', 'preference', 'summary')),
    CONSTRAINT chk_memory_entries_source CHECK (source IN ('explicit', 'inferred')),
    CONSTRAINT chk_memory_entries_confidence CHECK (confidence >= 0 AND confidence <= 1)
);
CREATE INDEX IF NOT EXISTS idx_memory_user ON memory_entries(user_id);
CREATE INDEX IF NOT EXISTS idx_memory_user_category ON memory_entries(user_id, category);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_user_key_context ON memory_entries(user_id, key, context);

-- ============================================================
-- 18. memory_cards
-- Spec base: Chapter 7.2.6A / 5.2.4
-- ============================================================
CREATE TABLE IF NOT EXISTS memory_cards (
    id                  VARCHAR(64) PRIMARY KEY,
    user_id             VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category            VARCHAR(16) NOT NULL,
    content             TEXT        NOT NULL DEFAULT '',
    excerpt             TEXT        NOT NULL DEFAULT '',
    backstory           TEXT        NOT NULL DEFAULT '',
    person              VARCHAR(128) NOT NULL DEFAULT '',
    relationship        VARCHAR(64)  NOT NULL DEFAULT '',
    context             VARCHAR(128) NOT NULL DEFAULT 'general',
    confidence          REAL         NOT NULL DEFAULT 1.0,
    object_id           VARCHAR(64) REFERENCES memory_objects(id) ON DELETE SET NULL,
    source_session_id   VARCHAR(64) REFERENCES sessions(id) ON DELETE SET NULL,
    created_at          BIGINT      NOT NULL,
    updated_at          BIGINT      NOT NULL,
    CONSTRAINT chk_memory_cards_category CHECK (category IN ('fact', 'preference', 'event', 'plan')),
    CONSTRAINT chk_memory_cards_confidence CHECK (confidence >= 0 AND confidence <= 1)
);
CREATE INDEX IF NOT EXISTS idx_memory_cards_user ON memory_cards(user_id);
CREATE INDEX IF NOT EXISTS idx_memory_cards_user_category ON memory_cards(user_id, category);
CREATE INDEX IF NOT EXISTS idx_memory_cards_user_context ON memory_cards(user_id, context);

-- ============================================================
-- 19. memory_dialogue_chunks
-- Spec base: Chapter 7.2.6B / 5.2.5
-- ============================================================
CREATE TABLE IF NOT EXISTS memory_dialogue_chunks (
    id                  VARCHAR(64) PRIMARY KEY,
    user_id             VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id          VARCHAR(64) REFERENCES sessions(id) ON DELETE SET NULL,
    turn_start          INT         NOT NULL,
    turn_end            INT         NOT NULL,
    context_prefix      TEXT        NOT NULL,
    content             TEXT        NOT NULL DEFAULT '',
    excerpt             TEXT        NOT NULL DEFAULT '',
    object_id           VARCHAR(64) REFERENCES memory_objects(id) ON DELETE SET NULL,
    participants        JSONB       NOT NULL DEFAULT '[]'::jsonb,
    intent_tags         JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at          BIGINT      NOT NULL,
    updated_at          BIGINT      NOT NULL,
    CONSTRAINT chk_memory_dialogue_chunks_turn_range CHECK (turn_start >= 0 AND turn_end >= turn_start)
);
CREATE INDEX IF NOT EXISTS idx_mem_chunks_user ON memory_dialogue_chunks(user_id);
CREATE INDEX IF NOT EXISTS idx_mem_chunks_user_session ON memory_dialogue_chunks(user_id, session_id);
CREATE INDEX IF NOT EXISTS idx_mem_chunks_user_updated ON memory_dialogue_chunks(user_id, updated_at);

-- ============================================================
-- 20. file_delete_jobs
-- Spec base: Chapter 7.4 trigger requirements
-- ============================================================
CREATE TABLE IF NOT EXISTS file_delete_jobs (
    id              BIGSERIAL PRIMARY KEY,
    file_id         VARCHAR(64) NOT NULL,
    storage_url     VARCHAR(1024) NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    retry_count     INT         NOT NULL DEFAULT 0,
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      BIGINT      NOT NULL,
    updated_at      BIGINT      NOT NULL,
    CONSTRAINT chk_file_delete_jobs_status CHECK (status IN ('pending', 'processing', 'success', 'failed')),
    CONSTRAINT chk_file_delete_jobs_retry_count CHECK (retry_count >= 0)
);
CREATE INDEX IF NOT EXISTS idx_file_delete_jobs_status ON file_delete_jobs(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_file_delete_jobs_file_id ON file_delete_jobs(file_id);

-- ============================================================
-- 21. memory_object_delete_jobs
-- Spec base: Chapter 7.4 trigger requirements
-- ============================================================
CREATE TABLE IF NOT EXISTS memory_object_delete_jobs (
    id              BIGSERIAL PRIMARY KEY,
    object_id       VARCHAR(64) NOT NULL,
    storage_key     VARCHAR(512) NOT NULL DEFAULT '',
    storage_url     VARCHAR(1024) NOT NULL DEFAULT '',
    status          VARCHAR(16) NOT NULL DEFAULT 'pending',
    retry_count     INT         NOT NULL DEFAULT 0,
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      BIGINT      NOT NULL,
    updated_at      BIGINT      NOT NULL,
    CONSTRAINT chk_memory_object_delete_jobs_status CHECK (status IN ('pending', 'processing', 'success', 'failed')),
    CONSTRAINT chk_memory_object_delete_jobs_retry_count CHECK (retry_count >= 0)
);
CREATE INDEX IF NOT EXISTS idx_memory_object_delete_jobs_status ON memory_object_delete_jobs(status, updated_at);
CREATE INDEX IF NOT EXISTS idx_memory_object_delete_jobs_object_id ON memory_object_delete_jobs(object_id);

-- ============================================================
-- 22. Cleanup job triggers
-- Writes delete jobs when rows are removed from files / memory_objects.
-- ============================================================
CREATE OR REPLACE FUNCTION enqueue_file_delete_job()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO file_delete_jobs (
        file_id,
        storage_url,
        status,
        retry_count,
        last_error,
        created_at,
        updated_at
    ) VALUES (
        OLD.id,
        OLD.storage_url,
        'pending',
        0,
        '',
        (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT,
        (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT
    );
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_enqueue_file_delete_job ON files;
CREATE TRIGGER trg_enqueue_file_delete_job
AFTER DELETE ON files
FOR EACH ROW
EXECUTE FUNCTION enqueue_file_delete_job();

CREATE OR REPLACE FUNCTION enqueue_memory_object_delete_job()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO memory_object_delete_jobs (
        object_id,
        storage_key,
        storage_url,
        status,
        retry_count,
        last_error,
        created_at,
        updated_at
    ) VALUES (
        OLD.id,
        OLD.storage_key,
        OLD.storage_url,
        'pending',
        0,
        '',
        (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT,
        (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT
    );
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_enqueue_memory_object_delete_job ON memory_objects;
CREATE TRIGGER trg_enqueue_memory_object_delete_job
AFTER DELETE ON memory_objects
FOR EACH ROW
EXECUTE FUNCTION enqueue_memory_object_delete_job();

-- ============================================================
-- 23. updated_at triggers
-- ============================================================
DROP TRIGGER IF EXISTS trg_users_set_updated_at ON users;
CREATE TRIGGER trg_users_set_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_pending_registrations_set_updated_at ON pending_registrations;
CREATE TRIGGER trg_pending_registrations_set_updated_at
BEFORE UPDATE ON pending_registrations
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_sessions_set_updated_at ON sessions;
CREATE TRIGGER trg_sessions_set_updated_at
BEFORE UPDATE ON sessions
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_tasks_set_updated_at ON tasks;
CREATE TRIGGER trg_tasks_set_updated_at
BEFORE UPDATE ON tasks
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_kb_collections_set_updated_at ON kb_collections;
CREATE TRIGGER trg_kb_collections_set_updated_at
BEFORE UPDATE ON kb_collections
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_kb_documents_set_updated_at ON kb_documents;
CREATE TRIGGER trg_kb_documents_set_updated_at
BEFORE UPDATE ON kb_documents
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_task_requirements_set_updated_at ON task_requirements;
CREATE TRIGGER trg_task_requirements_set_updated_at
BEFORE UPDATE ON task_requirements
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_task_pages_set_updated_at ON task_pages;
CREATE TRIGGER trg_task_pages_set_updated_at
BEFORE UPDATE ON task_pages
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_task_page_order_set_updated_at ON task_page_order;
CREATE TRIGGER trg_task_page_order_set_updated_at
BEFORE UPDATE ON task_page_order
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_task_reference_files_set_updated_at ON task_reference_files;
CREATE TRIGGER trg_task_reference_files_set_updated_at
BEFORE UPDATE ON task_reference_files
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_task_exports_set_updated_at ON task_exports;
CREATE TRIGGER trg_task_exports_set_updated_at
BEFORE UPDATE ON task_exports
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_suspended_pages_set_updated_at ON suspended_pages;
CREATE TRIGGER trg_suspended_pages_set_updated_at
BEFORE UPDATE ON suspended_pages
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_page_merge_states_set_updated_at ON page_merge_states;
CREATE TRIGGER trg_page_merge_states_set_updated_at
BEFORE UPDATE ON page_merge_states
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_memory_objects_set_updated_at ON memory_objects;
CREATE TRIGGER trg_memory_objects_set_updated_at
BEFORE UPDATE ON memory_objects
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_memory_entries_set_updated_at ON memory_entries;
CREATE TRIGGER trg_memory_entries_set_updated_at
BEFORE UPDATE ON memory_entries
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_memory_cards_set_updated_at ON memory_cards;
CREATE TRIGGER trg_memory_cards_set_updated_at
BEFORE UPDATE ON memory_cards
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_memory_dialogue_chunks_set_updated_at ON memory_dialogue_chunks;
CREATE TRIGGER trg_memory_dialogue_chunks_set_updated_at
BEFORE UPDATE ON memory_dialogue_chunks
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_file_delete_jobs_set_updated_at ON file_delete_jobs;
CREATE TRIGGER trg_file_delete_jobs_set_updated_at
BEFORE UPDATE ON file_delete_jobs
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

DROP TRIGGER IF EXISTS trg_memory_object_delete_jobs_set_updated_at ON memory_object_delete_jobs;
CREATE TRIGGER trg_memory_object_delete_jobs_set_updated_at
BEFORE UPDATE ON memory_object_delete_jobs
FOR EACH ROW EXECUTE FUNCTION set_updated_at_unix_ms();

COMMIT;
