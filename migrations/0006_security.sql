-- Additive migration: existing records are preserved.
CREATE TABLE auth_rate_limits (
    key TEXT PRIMARY KEY,
    hits INTEGER NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX auth_rate_limits_expiry ON auth_rate_limits(expires_at);
CREATE INDEX IF NOT EXISTS sessions_user ON sessions(user_id);
ALTER TABLE sessions ADD COLUMN token_hash TEXT;
CREATE UNIQUE INDEX sessions_token_hash ON sessions(token_hash) WHERE token_hash IS NOT NULL;
ALTER TABLE entries ADD CONSTRAINT entries_year_valid CHECK (report_year BETWEEN 2000 AND 2100) NOT VALID;
ALTER TABLE entries ADD CONSTRAINT entries_amount_valid CHECK (amount_rub >= 0) NOT VALID;
ALTER TABLE entries ADD CONSTRAINT entries_payload_object CHECK (jsonb_typeof(payload) = 'object') NOT VALID;
ALTER TABLE attachments ADD CONSTRAINT attachments_size_valid CHECK (size_bytes > 0 AND size_bytes <= 67108864) NOT VALID;
UPDATE settings SET value='60' WHERE key='audit_log_retention_days' AND value::integer < 60;
CREATE TABLE file_deletion_queue (id BIGSERIAL PRIMARY KEY, storage_path TEXT NOT NULL, queued_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE FUNCTION queue_deleted_attachment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO file_deletion_queue(storage_path) VALUES(OLD.storage_path);
  RETURN OLD;
END $$;
CREATE TRIGGER attachments_queue_deletion AFTER DELETE ON attachments FOR EACH ROW EXECUTE FUNCTION queue_deleted_attachment();
