-- STORE-05: состояние антивирусной проверки хранится вместе с файлом. Раньше
-- проверка была разовым событием при загрузке: заражённый файл не сохранялся,
-- но и «сканер не ответил» выглядело так же, а по самому вложению нельзя было
-- сказать, проверено ли оно вообще.
--
-- Заражённый файл в хранилище не попадает никогда, поэтому состояния rejected
-- в таблице нет: оно существует только как исход загрузки.
ALTER TABLE attachments
  ADD COLUMN scan_status TEXT NOT NULL DEFAULT 'clean',
  ADD COLUMN scan_signature TEXT,
  ADD COLUMN scanned_at TIMESTAMPTZ;

ALTER TABLE attachments
  ADD CONSTRAINT attachments_scan_status_check
    CHECK (scan_status IN ('clean','quarantined','unscanned')),
  ADD CONSTRAINT attachments_scan_signature_length
    CHECK (scan_signature IS NULL OR length(btrim(scan_signature)) BETWEEN 1 AND 200),
  -- Карантин без причины неразличим от чистого файла, поэтому у него всегда
  -- есть отметка времени проверки.
  ADD CONSTRAINT attachments_scan_consistency
    CHECK (scan_status <> 'quarantined' OR scanned_at IS NOT NULL);

-- Исторические вложения загружались до появления состояния: они помечаются
-- как непроверенные, а не выдаются за чистые.
UPDATE attachments SET scan_status='unscanned' WHERE scanned_at IS NULL;

CREATE INDEX attachments_scan_status_idx ON attachments(scan_status)
  WHERE scan_status <> 'clean';
