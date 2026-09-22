-- A report snapshot must identify the exact evidence bytes, not only file names.
-- Legacy rows stay nullable because their content may already be outside storage;
-- every upload made by the current application stores a SHA-256 digest.
ALTER TABLE attachments
  ADD COLUMN content_sha256 CHAR(64),
  ADD CONSTRAINT attachments_content_sha256_check CHECK (
    content_sha256 IS NULL OR content_sha256 ~ '^[0-9a-f]{64}$'
  );

CREATE INDEX attachments_content_sha256_idx
  ON attachments(content_sha256)
  WHERE content_sha256 IS NOT NULL;
