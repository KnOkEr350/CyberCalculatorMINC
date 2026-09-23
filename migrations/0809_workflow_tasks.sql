-- SEC-04 / SEC-10 (ADR-22): задачи workflow и их диспетчер.
--
-- Задача требует профильной роли. Диспетчер ищет исполнителя по цепочке
-- профильный специалист → закреплённый куратор → ORG_ADMIN → SUPER_ADMIN. Шаг
-- назначения — данные, а не поведение обработчика: маршрут, причина fallback и
-- каждая попытка переназначения остаются в истории.
CREATE TABLE workflow_tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  it_company_id UUID NOT NULL REFERENCES accredited_it_companies(id),
  partner_id UUID REFERENCES partners(id),
  kind TEXT NOT NULL CHECK (kind ~ '^[a-z][a-z0-9_.]{2,63}$'),
  title TEXT NOT NULL CHECK (length(btrim(title)) BETWEEN 3 AND 300),
  subject_type TEXT NOT NULL CHECK (subject_type ~ '^[a-z][a-z0-9_]{1,40}$'),
  subject_id TEXT NOT NULL CHECK (length(btrim(subject_id)) BETWEEN 1 AND 100),
  -- Что исполнитель должен уметь: профильная роль и полномочие, которое нужно
  -- для действия. Куратор получает задачу только если это полномочие делегируется.
  required_role TEXT NOT NULL CHECK (required_role IN
    ('hr_specialist','financial_specialist','legal_specialist','curator','org_admin','holding_admin','super_admin','auditor_viewer')),
  required_permission TEXT NOT NULL CHECK (length(btrim(required_permission)) BETWEEN 3 AND 80),
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','done','cancelled')),
  assignee_id UUID REFERENCES users(id),
  route TEXT NOT NULL CHECK (route IN ('specialist','curator','org_admin','super_admin','unassigned')),
  -- Задача ушла выше профильной роли и куратора: помечена для администрации.
  unassigned_escalated BOOLEAN NOT NULL DEFAULT FALSE,
  fallback_reason TEXT,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_by UUID REFERENCES users(id),
  completed_at TIMESTAMPTZ,
  CHECK ((route = 'unassigned') = (assignee_id IS NULL)),
  CHECK (unassigned_escalated = (route IN ('org_admin','super_admin','unassigned'))),
  CHECK (route = 'specialist' OR fallback_reason IS NOT NULL),
  CHECK ((status = 'done') = (completed_at IS NOT NULL AND completed_by IS NOT NULL))
);

-- Одна открытая задача на предмет и вид: повторное создание идемпотентно.
CREATE UNIQUE INDEX workflow_tasks_one_open_idx ON workflow_tasks (kind, subject_type, subject_id) WHERE status = 'open';
CREATE INDEX workflow_tasks_assignee_idx ON workflow_tasks (assignee_id, status) WHERE status = 'open';
CREATE INDEX workflow_tasks_tenant_idx ON workflow_tasks (it_company_id, status, created_at DESC);

CREATE TABLE workflow_task_events (
  id BIGSERIAL PRIMARY KEY,
  task_id UUID NOT NULL REFERENCES workflow_tasks(id) ON DELETE CASCADE,
  action TEXT NOT NULL CHECK (action IN ('created','reassigned','reassign_attempt','completed','cancelled')),
  from_assignee UUID REFERENCES users(id),
  to_assignee UUID REFERENCES users(id),
  route TEXT NOT NULL,
  reason TEXT,
  -- Пусто у действий системы (плановое переназначение).
  actor_id UUID REFERENCES users(id),
  system_action BOOLEAN NOT NULL DEFAULT FALSE,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (system_action = (actor_id IS NULL))
);
CREATE INDEX workflow_task_events_task_idx ON workflow_task_events (task_id, id);

CREATE OR REPLACE FUNCTION workflow_task_events_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN RETURN OLD; END IF;
  RAISE EXCEPTION 'workflow task events are append-only';
END $$;

CREATE TRIGGER workflow_task_events_append_only
BEFORE UPDATE OR DELETE ON workflow_task_events
FOR EACH ROW EXECUTE FUNCTION workflow_task_events_guard();
