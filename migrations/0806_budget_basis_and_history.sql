-- DATA-11: норматив 3% хранится вместе со своим основанием.
--
-- Номер из диапазона отчётности, а не справочников: база экономии и дата
-- доведения добавлены в 0801, поэтому эта миграция обязана идти после неё. Раньше в строке
-- были база и дата доведения, но не было ни года, к которому относится база,
-- ни подтверждения ФНС, ни истории изменений: пересчёт норматива задним числом
-- не оставлял следа.
ALTER TABLE organization_budget_targets
  ADD COLUMN basis_year INTEGER,
  ADD COLUMN fns_confirmed_at DATE,
  ADD COLUMN fns_reference TEXT;

-- База считается по году N-2 (Приказ № 270): год основания выводится из
-- отчётного и не может быть указан произвольно.
UPDATE organization_budget_targets SET basis_year = report_year - 2 WHERE basis_year IS NULL;

ALTER TABLE organization_budget_targets
  ADD CONSTRAINT organization_budget_basis_year_check
    CHECK (basis_year IS NULL OR basis_year = report_year - 2),
  ADD CONSTRAINT organization_budget_fns_reference_length
    CHECK (fns_reference IS NULL OR length(btrim(fns_reference)) BETWEEN 1 AND 2000),
  -- Подтверждение ФНС — это дата и реквизит вместе: одна дата без документа
  -- ничего не подтверждает.
  ADD CONSTRAINT organization_budget_fns_consistency
    CHECK ((fns_confirmed_at IS NULL AND fns_reference IS NULL)
        OR (fns_confirmed_at IS NOT NULL AND fns_reference IS NOT NULL));

-- История норматива: каждое изменение сохраняется целиком, включая прежние
-- значения. Таблица только дополняется, поэтому переписать основание норматива
-- задним числом нельзя.
CREATE TABLE organization_budget_target_history (
  id BIGSERIAL PRIMARY KEY,
  it_company_id UUID REFERENCES accredited_it_companies(id),
  report_year INTEGER NOT NULL CHECK(report_year BETWEEN 2000 AND 2100),
  basis_year INTEGER,
  target_amount_rub NUMERIC(16,2) NOT NULL,
  savings_base_rub NUMERIC(16,2),
  source_reference TEXT,
  notified_at DATE,
  fns_confirmed_at DATE,
  fns_reference TEXT,
  operation TEXT NOT NULL CHECK(operation IN ('insert','update')),
  changed_by UUID REFERENCES users(id),
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX organization_budget_target_history_idx
  ON organization_budget_target_history(it_company_id, report_year, changed_at DESC);

CREATE OR REPLACE FUNCTION prevent_budget_history_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'budget target history is append-only';
END $$;

CREATE TRIGGER organization_budget_target_history_append_only
BEFORE UPDATE OR DELETE ON organization_budget_target_history
FOR EACH ROW EXECUTE FUNCTION prevent_budget_history_mutation();

CREATE OR REPLACE FUNCTION record_budget_target_history() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO public.organization_budget_target_history(
    it_company_id,report_year,basis_year,target_amount_rub,savings_base_rub,
    source_reference,notified_at,fns_confirmed_at,fns_reference,operation,changed_by)
  VALUES (NEW.it_company_id,NEW.report_year,NEW.basis_year,NEW.target_amount_rub,
    NEW.savings_base_rub,NEW.source_reference,NEW.notified_at,NEW.fns_confirmed_at,
    NEW.fns_reference,lower(TG_OP),NEW.updated_by);
  RETURN NEW;
END $$;

CREATE TRIGGER organization_budget_targets_history
AFTER INSERT OR UPDATE ON organization_budget_targets
FOR EACH ROW EXECUTE FUNCTION record_budget_target_history();

-- Действующие строки попадают в историю как исходное состояние: иначе первая
-- же правка выглядела бы изменением из ниоткуда.
INSERT INTO organization_budget_target_history(
  it_company_id,report_year,basis_year,target_amount_rub,savings_base_rub,
  source_reference,notified_at,fns_confirmed_at,fns_reference,operation,changed_by,changed_at)
SELECT it_company_id,report_year,basis_year,target_amount_rub,savings_base_rub,
  source_reference,notified_at,fns_confirmed_at,fns_reference,'insert',updated_by,updated_at
FROM organization_budget_targets;
