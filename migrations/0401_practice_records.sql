-- PRA-01 / PRA-06: самостоятельная типизированная карточка практиканта и
-- безопасный backfill старых записей. JSON payload остаётся совместимым API,
-- а таблица ниже становится строгой проекцией домена практики; триггер не
-- позволяет ей разойтись с исходной записью.
CREATE TABLE practice_records (
  entry_id UUID PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
  -- Старые записи могли не иметь этих ссылок; они сохраняются в
  -- manual_review, а полный состав требует обе связи.
  partner_id UUID REFERENCES partners(id),
  agreement_id UUID REFERENCES agreements(id),
  student_full_name TEXT,
  specialty_code TEXT,
  course TEXT,
  period_start DATE,
  period_end DATE,
  mentor_id UUID REFERENCES mentors(id),
  labor_contract_type TEXT,
  labor_contract_number TEXT,
  labor_contract_date DATE,
  practice_agreement_number TEXT,
  practice_agreement_date DATE,
  practice_agreement_start_date DATE,
  practice_agreement_end_date DATE,
  student_age SMALLINT,
  weekly_hours NUMERIC(5,2),
  backfill_status TEXT NOT NULL CHECK (backfill_status IN ('complete','manual_review')),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (period_end IS NULL OR period_start IS NULL OR period_end >= period_start),
  CHECK (practice_agreement_end_date IS NULL OR practice_agreement_start_date IS NULL
    OR practice_agreement_end_date >= practice_agreement_start_date),
  CHECK (student_age IS NULL OR student_age BETWEEN 14 AND 100),
  CHECK (weekly_hours IS NULL OR weekly_hours > 0 AND weekly_hours <= 40),
  CHECK (backfill_status = 'manual_review' OR (
    student_full_name IS NOT NULL AND length(btrim(student_full_name)) BETWEEN 2 AND 200
    AND specialty_code IS NOT NULL AND length(btrim(specialty_code)) BETWEEN 2 AND 50
    AND course IS NOT NULL AND length(btrim(course)) BETWEEN 1 AND 30
    AND period_start IS NOT NULL AND period_end IS NOT NULL AND period_end >= period_start
    AND mentor_id IS NOT NULL
    AND labor_contract_type = 'fixed_term'
    AND labor_contract_number IS NOT NULL AND length(btrim(labor_contract_number)) BETWEEN 1 AND 100
    AND labor_contract_date IS NOT NULL
    AND practice_agreement_number IS NOT NULL AND length(btrim(practice_agreement_number)) BETWEEN 1 AND 100
    AND practice_agreement_date IS NOT NULL
    AND student_age IS NOT NULL AND weekly_hours IS NOT NULL
  ))
);

CREATE OR REPLACE FUNCTION enforce_practice_entry_category() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS(SELECT 1 FROM entries e WHERE e.id=NEW.entry_id AND e.category_code='employment_practice') THEN
    RAISE EXCEPTION 'practice_records accepts only employment_practice entries';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER practice_records_category_guard_trg
BEFORE INSERT OR UPDATE OF entry_id ON practice_records
FOR EACH ROW EXECUTE FUNCTION enforce_practice_entry_category();

CREATE OR REPLACE FUNCTION sync_practice_record() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  v_student TEXT := NULLIF(btrim(COALESCE(NEW.payload->>'student_full_name','')), '');
  v_specialty TEXT := NULLIF(btrim(COALESCE(NEW.payload->>'specialty_code','')), '');
  v_course TEXT := NULLIF(btrim(COALESCE(NEW.payload->>'course','')), '');
  v_period_start DATE;
  v_period_end DATE;
  v_contract_date DATE;
  v_practice_date DATE;
  v_practice_start DATE;
  v_practice_end DATE;
  v_age NUMERIC;
  v_hours NUMERIC;
  v_status TEXT;
BEGIN
  IF NEW.category_code <> 'employment_practice' THEN
    DELETE FROM practice_records WHERE entry_id = NEW.id;
    RETURN NEW;
  END IF;

  IF pg_input_is_valid(NEW.payload->>'period_start','date') THEN v_period_start := (NEW.payload->>'period_start')::date; END IF;
  IF pg_input_is_valid(NEW.payload->>'period_end','date') THEN v_period_end := (NEW.payload->>'period_end')::date; END IF;
  IF pg_input_is_valid(NEW.payload->>'labor_contract_date','date') THEN v_contract_date := (NEW.payload->>'labor_contract_date')::date; END IF;
  IF pg_input_is_valid(NEW.payload->>'practice_agreement_date','date') THEN v_practice_date := (NEW.payload->>'practice_agreement_date')::date; END IF;
  IF pg_input_is_valid(NEW.payload->>'practice_agreement_start_date','date') THEN v_practice_start := (NEW.payload->>'practice_agreement_start_date')::date; END IF;
  IF pg_input_is_valid(NEW.payload->>'practice_agreement_end_date','date') THEN v_practice_end := (NEW.payload->>'practice_agreement_end_date')::date; END IF;
  IF pg_input_is_valid(NEW.payload->>'student_age','numeric') THEN v_age := (NEW.payload->>'student_age')::numeric; END IF;
  IF pg_input_is_valid(NEW.payload->>'weekly_hours','numeric') THEN v_hours := (NEW.payload->>'weekly_hours')::numeric; END IF;

  v_status := CASE WHEN
    NEW.partner_id IS NOT NULL AND NEW.agreement_id IS NOT NULL
    AND v_student IS NOT NULL AND length(v_student) BETWEEN 2 AND 200
    AND v_specialty IS NOT NULL AND length(v_specialty) BETWEEN 2 AND 50
    AND v_course IS NOT NULL AND length(v_course) BETWEEN 1 AND 30
    AND v_period_start IS NOT NULL AND v_period_end IS NOT NULL AND v_period_end >= v_period_start
    AND NEW.mentor_id IS NOT NULL
    AND lower(btrim(COALESCE(NEW.payload->>'labor_contract_type',''))) = 'fixed_term'
    AND NULLIF(btrim(COALESCE(NEW.payload->>'labor_contract_number','')), '') IS NOT NULL
    AND v_contract_date IS NOT NULL
    AND NULLIF(btrim(COALESCE(NEW.payload->>'practice_agreement_number','')), '') IS NOT NULL
    AND v_practice_date IS NOT NULL
    AND v_age IS NOT NULL AND trunc(v_age) = v_age AND v_age BETWEEN 14 AND 100
    AND v_hours IS NOT NULL AND v_hours > 0 AND v_hours <= 40
    AND (v_practice_start IS NULL OR v_practice_end IS NULL OR v_practice_end >= v_practice_start)
    THEN 'complete' ELSE 'manual_review' END;

  INSERT INTO practice_records(entry_id,partner_id,agreement_id,student_full_name,specialty_code,course,
    period_start,period_end,mentor_id,labor_contract_type,labor_contract_number,labor_contract_date,
    practice_agreement_number,practice_agreement_date,practice_agreement_start_date,practice_agreement_end_date,
    student_age,weekly_hours,backfill_status,updated_at)
  VALUES(NEW.id,NEW.partner_id,NEW.agreement_id,v_student,v_specialty,v_course,v_period_start,v_period_end,NEW.mentor_id,
    NULLIF(lower(btrim(COALESCE(NEW.payload->>'labor_contract_type',''))),''),
    NULLIF(btrim(COALESCE(NEW.payload->>'labor_contract_number','')),''),v_contract_date,
    NULLIF(btrim(COALESCE(NEW.payload->>'practice_agreement_number','')),''),v_practice_date,v_practice_start,v_practice_end,
    CASE WHEN v_age IS NOT NULL AND trunc(v_age)=v_age AND v_age BETWEEN 14 AND 100 THEN v_age::smallint END,
    CASE WHEN v_hours > 0 AND v_hours <= 40 THEN v_hours END,v_status,now())
  ON CONFLICT(entry_id) DO UPDATE SET
    partner_id=EXCLUDED.partner_id,agreement_id=EXCLUDED.agreement_id,student_full_name=EXCLUDED.student_full_name,
    specialty_code=EXCLUDED.specialty_code,course=EXCLUDED.course,period_start=EXCLUDED.period_start,
    period_end=EXCLUDED.period_end,mentor_id=EXCLUDED.mentor_id,labor_contract_type=EXCLUDED.labor_contract_type,
    labor_contract_number=EXCLUDED.labor_contract_number,labor_contract_date=EXCLUDED.labor_contract_date,
    practice_agreement_number=EXCLUDED.practice_agreement_number,practice_agreement_date=EXCLUDED.practice_agreement_date,
    practice_agreement_start_date=EXCLUDED.practice_agreement_start_date,
    practice_agreement_end_date=EXCLUDED.practice_agreement_end_date,student_age=EXCLUDED.student_age,
    weekly_hours=EXCLUDED.weekly_hours,backfill_status=EXCLUDED.backfill_status,updated_at=now();

  IF v_status = 'complete' THEN
    UPDATE legacy_backfill_findings SET resolved_at=now(),
      resolved_reason='Карточка практиканта дополнена через штатный интерфейс'
    WHERE entry_id=NEW.id AND rule_code='practice.model_incomplete' AND resolved_at IS NULL;
  ELSE
    INSERT INTO legacy_backfill_findings(entry_id,category_code,rule_code,detail)
    VALUES(NEW.id,'employment_practice','practice.model_incomplete',
      'Типизированная карточка практиканта неполна; проверьте ФИО, специальность, курс, период, наставника, срочный ТД, договор практической подготовки, возраст и недельные часы')
    ON CONFLICT(entry_id,rule_code) DO UPDATE SET detail=EXCLUDED.detail,resolved_at=NULL,resolved_reason=NULL,detected_at=now();
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER entries_practice_record_sync_trg
AFTER INSERT OR UPDATE OF category_code,partner_id,agreement_id,payload,mentor_id ON entries
FOR EACH ROW EXECUTE FUNCTION sync_practice_record();

-- Идемпотентный backfill через тот же sync-контракт. Производные данные не
-- должны возвращать утверждённый отчёт в черновик.
ALTER TABLE entries DISABLE TRIGGER entries_reset_report_workflow;
UPDATE entries SET payload=payload WHERE category_code='employment_practice';
ALTER TABLE entries ENABLE TRIGGER entries_reset_report_workflow;

INSERT INTO legacy_backfill_findings(entry_id,category_code,rule_code,detail)
SELECT p.entry_id,'employment_practice','practice.model_incomplete',
  'Типизированная карточка практиканта неполна; проверьте ФИО, специальность, курс, период, наставника, срочный ТД, договор практической подготовки, возраст и недельные часы'
FROM practice_records p WHERE p.backfill_status='manual_review'
ON CONFLICT(entry_id,rule_code) DO NOTHING;

CREATE INDEX practice_records_scope_idx ON practice_records(partner_id,period_start,period_end);
CREATE INDEX practice_records_review_idx ON practice_records(backfill_status) WHERE backfill_status='manual_review';
