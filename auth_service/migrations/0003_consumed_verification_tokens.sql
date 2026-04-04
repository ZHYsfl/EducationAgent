CREATE TABLE IF NOT EXISTS consumed_verification_tokens (
    verification_token_hash VARCHAR(128) PRIMARY KEY,
    user_id                 VARCHAR(64) NOT NULL,
    consumed_at             BIGINT NOT NULL
);
