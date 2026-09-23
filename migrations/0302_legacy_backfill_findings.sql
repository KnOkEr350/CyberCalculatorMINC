-- TCH-08: дозаполнение старых записей и пометки на ручное уточнение.
--
-- Записи, внесённые до появления справочника сотрудников, не связаны с ним.
-- Дозаполнение связывает то, что определяется однозначно, а всё остальное
-- оставляет как есть и помечает здесь: недостающие сведения не выдумываются, и
-- оператор видит, что именно и почему требует его решения.
CREATE TABLE legacy_backfill_findings (
  id BIGSERIAL PRIMARY KEY,
  entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  category_code TEXT NOT NULL,
  rule_code TEXT NOT NULL CHECK (rule_code ~ '^[a-z_]+(\.[a-z_]+)+$'),
  detail TEXT NOT NULL CHECK (length(btrim(detail)) BETWEEN 1 AND 1000),
  -- Предложение, которое система не применила сама (например, найденный, но
  -- не связанный сотрудник): применить его должен человек.
  suggested_value TEXT,
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  resolved_reason TEXT,
  UNIQUE (entry_id, rule_code),
  CHECK ((resolved_at IS NULL) = (resolved_reason IS NULL))
);

CREATE INDEX legacy_backfill_findings_open_idx
  ON legacy_backfill_findings(category_code, rule_code) WHERE resolved_at IS NULL;
