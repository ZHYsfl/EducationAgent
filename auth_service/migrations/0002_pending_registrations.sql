CREATE TABLE IF NOT EXISTS pending_registrations (
    user_id                  VARCHAR(64) PRIMARY KEY,
    username                 VARCHAR(64) NOT NULL UNIQUE,
    email                    VARCHAR(128) NOT NULL UNIQUE,
    password_hash            VARCHAR(256) NOT NULL,
    display_name             VARCHAR(128) DEFAULT '',
    subject                  VARCHAR(64) DEFAULT '',
    school                   VARCHAR(128) DEFAULT '',
    role                     VARCHAR(16) DEFAULT 'teacher',
    verification_token_hash  VARCHAR(128) NOT NULL UNIQUE,
    verification_expires_at  BIGINT NOT NULL,
    verification_sent_at     BIGINT NOT NULL,
    created_at               BIGINT NOT NULL,
    updated_at               BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pending_regs_expires_at ON pending_registrations(verification_expires_at);
