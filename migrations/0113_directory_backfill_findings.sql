-- DATA-13: отчёт о неполных и неоднозначных строках справочников после переноса.
--
-- Записи мероприятий уже имеют свой журнал находок (0302); справочники —
-- партнёры, соглашения, ИТ-компании — получают такой же: определяется то, что
-- перенесено с пробелами, а сами данные не переписываются. Находка исчезает из
-- открытых, когда причина исправлена, но остаётся в истории.
CREATE TABLE directory_backfill_findings (
  id BIGSERIAL PRIMARY KEY,
  entity_type TEXT NOT NULL CHECK (entity_type IN ('partner', 'agreement', 'it_company')),
  entity_id UUID NOT NULL,
  -- Арендатор находки; пусто у справочника, не привязанного к ИТ-компании.
  it_company_id UUID,
  rule_code TEXT NOT NULL CHECK (rule_code ~ '^[a-z_]+(\.[a-z_]+)+$'),
  detail TEXT NOT NULL CHECK (length(btrim(detail)) BETWEEN 1 AND 1000),
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  UNIQUE (entity_type, entity_id, rule_code)
);
CREATE INDEX directory_backfill_findings_open_idx ON directory_backfill_findings (it_company_id, entity_type, rule_code) WHERE resolved_at IS NULL;
