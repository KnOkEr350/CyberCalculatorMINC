-- WF-01 / WF-03 / WF-04 / WF-05: регламентный процесс Приказа № 270.
--
-- Проект соглашения, предварительный и итоговый перечни проходят одну цепочку
-- «рассмотрение → доработка → повторное рассмотрение» с календарными сроками
-- (ADR-02) и согласованием по молчанию. Состояние процесса и каждый переход
-- хранятся в БД: срок должен пережить перезапуск, а согласование по молчанию —
-- оставить след, отличимый от решения человека.
CREATE TABLE regulatory_processes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind TEXT NOT NULL CHECK (kind IN ('agreement','preliminary','final')),
  agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
  report_year INTEGER NOT NULL CHECK (report_year BETWEEN 2000 AND 2100),
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN
    ('draft','sent','in_review','rework','resubmitted','approved','default_approved','disputed')),
  -- Окно срока: какое из трёх идёт сейчас и когда оно истекает.
  window_kind TEXT CHECK (window_kind IN ('review','rework','rereview')),
  due_date DATE,
  expires_at TIMESTAMPTZ,
  round INTEGER NOT NULL DEFAULT 0 CHECK (round BETWEEN 0 AND 2),
  sent_at TIMESTAMPTZ,
  sent_late BOOLEAN NOT NULL DEFAULT FALSE,
  remarks TEXT,
  rework_lapsed_at TIMESTAMPTZ,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (kind, agreement_id, report_year),
  -- Открытое окно всегда имеет срок, закрытое — не имеет.
  CHECK ((window_kind IS NULL) = (due_date IS NULL) AND (window_kind IS NULL) = (expires_at IS NULL)),
  CHECK (status NOT IN ('approved','default_approved','disputed') OR window_kind IS NULL),
  CHECK ((status = 'draft') = (round = 0))
);

-- Просроченные окна выбирает фоновое задание: индекс только по открытым.
CREATE INDEX regulatory_processes_due_idx ON regulatory_processes(expires_at) WHERE window_kind IS NOT NULL;
CREATE INDEX regulatory_processes_agreement_idx ON regulatory_processes(agreement_id, report_year);

CREATE TABLE regulatory_process_events (
  id BIGSERIAL PRIMARY KEY,
  process_id UUID NOT NULL REFERENCES regulatory_processes(id) ON DELETE CASCADE,
  action TEXT NOT NULL,
  from_status TEXT NOT NULL,
  to_status TEXT NOT NULL,
  reason TEXT,
  due_date DATE,
  -- Пусто у действий системы: согласование по молчанию не приписывается человеку.
  actor_id UUID REFERENCES users(id),
  system_action BOOLEAN NOT NULL DEFAULT FALSE,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (system_action = (actor_id IS NULL))
);

CREATE INDEX regulatory_process_events_process_idx ON regulatory_process_events(process_id, id);

-- История переходов только дополняется.
CREATE OR REPLACE FUNCTION prevent_regulatory_event_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'regulatory process events are append-only';
END $$;

CREATE TRIGGER regulatory_process_events_append_only
BEFORE UPDATE OR DELETE ON regulatory_process_events
FOR EACH ROW EXECUTE FUNCTION prevent_regulatory_event_mutation();
