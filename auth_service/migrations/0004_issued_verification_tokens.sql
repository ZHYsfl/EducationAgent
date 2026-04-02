CREATE TABLE IF NOT EXISTS issued_verification_tokens (
    verification_token_hash VARCHAR(128) PRIMARY KEY,
    expires_at              BIGINT NOT NULL,
    created_at              BIGINT NOT NULL
);
