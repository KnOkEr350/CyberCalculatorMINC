-- Manual review metadata for educational-organization registry records.
ALTER TABLE education_directory
  ADD COLUMN verified_by UUID REFERENCES users(id);

-- Monitoring-only seed rows are visible as requiring review. Previously
-- confirmed rows retain their status and confirmation timestamp.
UPDATE education_directory
SET verification_status = 'pending', verified_at = NULL, verified_by = NULL
WHERE verification_status = 'monitoring_only';

-- Every row needs a stable key so that an exported and edited workbook can be
-- imported without relying on mutable organization names.
UPDATE education_directory
SET registry_record_id = 'directory:' || id::text
WHERE registry_record_id IS NULL OR registry_record_id = '';

CREATE INDEX education_directory_verified_by_idx
  ON education_directory(verified_by)
  WHERE verified_by IS NOT NULL;
