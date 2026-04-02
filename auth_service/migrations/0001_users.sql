CREATE TABLE IF NOT EXISTS users (
    id            VARCHAR(64) PRIMARY KEY,
    username      VARCHAR(64) NOT NULL UNIQUE,
    email         VARCHAR(128) NOT NULL UNIQUE,
    password_hash VARCHAR(256) NOT NULL,
    display_name  VARCHAR(128) DEFAULT '',
    subject       VARCHAR(64) DEFAULT '',
    school        VARCHAR(128) DEFAULT '',
    role          VARCHAR(16) DEFAULT 'teacher',
    created_at    BIGINT NOT NULL,
    updated_at    BIGINT NOT NULL
);
