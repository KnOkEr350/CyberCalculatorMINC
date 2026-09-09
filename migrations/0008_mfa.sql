ALTER TABLE users ADD COLUMN mfa_secret TEXT;
ALTER TABLE users ADD COLUMN mfa_pending_secret TEXT;
ALTER TABLE users ADD COLUMN mfa_pending_expires TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN mfa_last_counter BIGINT NOT NULL DEFAULT -1;
CREATE TABLE mfa_recovery_codes (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    PRIMARY KEY(user_id,code_hash)
);
