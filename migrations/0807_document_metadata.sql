-- DATA-12: typed document links, signature metadata and legal-dispute state.
ALTER TABLE attachments
  ADD COLUMN owner_type TEXT NOT NULL DEFAULT 'entry' CHECK (owner_type IN ('entry')),
  ADD COLUMN owner_id UUID,
  ADD COLUMN document_date DATE,
  ADD COLUMN valid_from DATE,
  ADD COLUMN valid_until DATE,
  ADD COLUMN signer_name TEXT,
  ADD COLUMN signer_certificate_id TEXT,
  ADD COLUMN signer_key_id TEXT,
  ADD COLUMN signature_algorithm TEXT,
  ADD COLUMN legal_dispute BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN dispute_reason TEXT,
  ADD COLUMN disputed_by UUID REFERENCES users(id),
  ADD COLUMN disputed_at TIMESTAMPTZ,
  ADD COLUMN metadata_version BIGINT NOT NULL DEFAULT 1;

UPDATE attachments SET owner_id=entry_id WHERE owner_id IS NULL;
ALTER TABLE attachments ALTER COLUMN owner_id SET NOT NULL;
ALTER TABLE attachments ADD CONSTRAINT attachments_owner_matches_entry CHECK(owner_id=entry_id);
ALTER TABLE attachments ADD CONSTRAINT attachments_validity_check
  CHECK(valid_until IS NULL OR valid_from IS NULL OR valid_until>=valid_from);
ALTER TABLE attachments ADD CONSTRAINT attachments_dispute_consistency CHECK(
  (NOT legal_dispute AND dispute_reason IS NULL AND disputed_by IS NULL AND disputed_at IS NULL)
  OR (legal_dispute AND length(trim(COALESCE(dispute_reason,'')))>0 AND disputed_by IS NOT NULL AND disputed_at IS NOT NULL)
);
CREATE INDEX attachments_owner_type_idx ON attachments(owner_type,owner_id,document_type);

CREATE FUNCTION ensure_attachment_entry_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.owner_type='entry' AND NEW.owner_id IS NULL THEN NEW.owner_id:=NEW.entry_id; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER attachments_ensure_owner
BEFORE INSERT OR UPDATE OF entry_id,owner_type,owner_id ON attachments
FOR EACH ROW EXECUTE FUNCTION ensure_attachment_entry_owner();
