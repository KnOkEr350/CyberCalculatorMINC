-- RISK-08: кэш агрегатов вклада мероприятий.
--
-- Таблица производная и одноразовая: её можно очистить и пересобрать целиком,
-- источником истины остаются entries и оси проверки. Пересборка идемпотентна и
-- атомарна по паре (арендатор, год): читатель видит либо старый срез, либо
-- новый, но не смесь. Суммы в копейках — NUMERIC(20,2), как у money.Amount.
CREATE TABLE activity_aggregates (
  tenant_id UUID NOT NULL,
  report_year INTEGER NOT NULL CHECK (report_year BETWEEN 2000 AND 2100),
  partner_id TEXT NOT NULL DEFAULT '',
  agreement_id TEXT NOT NULL DEFAULT '',
  category_code TEXT NOT NULL,
  audience TEXT NOT NULL,
  plan_entries INTEGER NOT NULL CHECK (plan_entries >= 0),
  fact_entries INTEGER NOT NULL CHECK (fact_entries >= 0),
  plan_units DOUBLE PRECISION NOT NULL,
  fact_units DOUBLE PRECISION NOT NULL,
  plan_amount_rub NUMERIC(20,2) NOT NULL,
  fact_amount_rub NUMERIC(20,2) NOT NULL,
  calculated_fact_rub NUMERIC(20,2) NOT NULL,
  confirmed_fact_rub NUMERIC(20,2) NOT NULL,
  counted_fact_rub NUMERIC(20,2) NOT NULL,
  eligible_plan_rub NUMERIC(20,2) NOT NULL,
  eligible_fact_rub NUMERIC(20,2) NOT NULL,
  incomplete_entries INTEGER NOT NULL CHECK (incomplete_entries >= 0),
  built_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, report_year, partner_id, agreement_id, category_code, audience)
);

-- Когда и по скольким записям собран срез: по этому видно, что кэш устарел.
CREATE TABLE activity_aggregate_builds (
  tenant_id UUID NOT NULL,
  report_year INTEGER NOT NULL,
  contributions INTEGER NOT NULL CHECK (contributions >= 0),
  built_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, report_year)
);
