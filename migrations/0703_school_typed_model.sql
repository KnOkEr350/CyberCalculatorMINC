-- SCH-01 / SCH-08: общая типизированная модель школьного трека (Виды 6, 7, 8) и
-- перенос старых записей.
--
-- Программа или платформа, ссылки на соглашение со школой/РОИВ, реестр групп
-- и акт, период выгрузки цифрового следа и источник финансирования раньше
-- жили только в payload. Теперь это типизированные колонки, которые выводит
-- триггер из проверенного payload (как для ООП/РПД в 0501 и ТОП-ИТ в 0601):
-- расхождение с payload невозможно, а пути записи не меняются.
ALTER TABLE entries
  ADD COLUMN school_program_name TEXT,
  ADD COLUMN school_class_range TEXT,
  ADD COLUMN school_agreement_reference TEXT,
  ADD COLUMN school_groups_reference TEXT,
  ADD COLUMN school_act_reference TEXT,
  ADD COLUMN school_period_start DATE,
  ADD COLUMN school_period_end DATE,
  ADD COLUMN school_funding_source TEXT,
  ADD COLUMN school_budget_funding TEXT CHECK (school_budget_funding IS NULL OR school_budget_funding IN ('absent', 'full_or_partial')),
  ADD COLUMN school_citizen_funding TEXT CHECK (school_citizen_funding IS NULL OR school_citizen_funding IN ('absent', 'full_or_partial')),
  -- Не определена программа или платформа.
  ADD COLUMN school_incomplete BOOLEAN NOT NULL DEFAULT FALSE;

CREATE OR REPLACE FUNCTION entries_school_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  program_key TEXT; budget TEXT; citizen TEXT; period_from TEXT; period_to TEXT;
BEGIN
  IF NEW.category_code NOT IN ('it_clubs', 'teacher_training', 'edu_content') THEN
    NEW.school_program_name := NULL; NEW.school_class_range := NULL; NEW.school_agreement_reference := NULL;
    NEW.school_groups_reference := NULL; NEW.school_act_reference := NULL; NEW.school_period_start := NULL;
    NEW.school_period_end := NULL; NEW.school_funding_source := NULL; NEW.school_budget_funding := NULL;
    NEW.school_citizen_funding := NULL; NEW.school_incomplete := FALSE;
    RETURN NEW;
  END IF;
  program_key := CASE WHEN NEW.category_code = 'edu_content' THEN 'platform_name' ELSE 'program_name' END;
  budget := lower(btrim(COALESCE(NEW.payload->>'budget_funding', '')));
  citizen := lower(btrim(COALESCE(NEW.payload->>'citizen_funding', '')));
  period_from := btrim(COALESCE(NEW.payload->>'digital_trace_period_start', ''));
  period_to := btrim(COALESCE(NEW.payload->>'digital_trace_period_end', ''));
  NEW.school_program_name := NULLIF(left(btrim(COALESCE(NEW.payload->>program_key, '')), 500), '');
  NEW.school_class_range := NULLIF(left(btrim(COALESCE(NEW.payload->>'class_range', '')), 100), '');
  NEW.school_agreement_reference := NULLIF(left(btrim(COALESCE(NEW.payload->>'school_agreement_reference', '')), 1000), '');
  NEW.school_groups_reference := NULLIF(left(btrim(COALESCE(NEW.payload->>'participant_groups_reference', '')), 1000), '');
  NEW.school_act_reference := NULLIF(left(btrim(COALESCE(NEW.payload->>'acceptance_act_reference', '')), 1000), '');
  NEW.school_period_start := CASE WHEN period_from ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN period_from::date END;
  NEW.school_period_end := CASE WHEN period_to ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN period_to::date END;
  NEW.school_funding_source := NULLIF(left(btrim(COALESCE(NEW.payload->>'funding_source', '')), 500), '');
  NEW.school_budget_funding := CASE WHEN budget IN ('absent', 'full_or_partial') THEN budget END;
  NEW.school_citizen_funding := CASE WHEN citizen IN ('absent', 'full_or_partial') THEN citizen END;
  NEW.school_incomplete := NEW.school_program_name IS NULL;
  RETURN NEW;
END $$;

CREATE TRIGGER entries_school_sync_trg
BEFORE INSERT OR UPDATE ON entries
FOR EACH ROW EXECUTE FUNCTION entries_school_sync();

-- SCH-08: перенос старых записей. Меняются только производные колонки, поэтому
-- триггер пересчёта статуса отчёта на время заполнения выключен — иначе
-- утверждённые отчёты вернулись бы в черновик.
ALTER TABLE entries DISABLE TRIGGER entries_reset_report_workflow;
UPDATE entries SET payload = payload WHERE category_code IN ('it_clubs', 'teacher_training', 'edu_content');
ALTER TABLE entries ENABLE TRIGGER entries_reset_report_workflow;

-- Что не удалось перенести или что нарушает действующие правила. Записи не
-- меняются: решение по каждой принимает человек.
INSERT INTO legacy_backfill_findings(entry_id, category_code, rule_code, detail)
SELECT id, category_code, 'school.program_missing',
  CASE WHEN category_code = 'edu_content' THEN 'Не указано наименование образовательной платформы'
       ELSE 'Не указано наименование программы' END
FROM entries WHERE category_code IN ('it_clubs', 'teacher_training', 'edu_content') AND school_incomplete
ON CONFLICT (entry_id, rule_code) DO NOTHING;

INSERT INTO legacy_backfill_findings(entry_id, category_code, rule_code, detail)
SELECT id, category_code, 'school.budget_funding_declared',
  'Заявлено бюджетное финансирование или средства граждан: такие мероприятия в зачёт не идут (SCH-06)'
FROM entries
WHERE category_code IN ('it_clubs', 'teacher_training', 'edu_content')
  AND (school_budget_funding = 'full_or_partial' OR school_citizen_funding = 'full_or_partial')
ON CONFLICT (entry_id, rule_code) DO NOTHING;

INSERT INTO legacy_backfill_findings(entry_id, category_code, rule_code, detail)
SELECT id, category_code, 'school.audience_mismatch',
  'Школьный трек, а аудитория записи — ' || audience
FROM entries
WHERE category_code IN ('it_clubs', 'teacher_training', 'edu_content') AND audience <> 'school'
ON CONFLICT (entry_id, rule_code) DO NOTHING;

INSERT INTO legacy_backfill_findings(entry_id, category_code, rule_code, detail)
SELECT id, category_code, 'school.digital_trace_incomplete',
  'Не определён корректный период выгрузки цифрового следа (Вид 8)'
FROM entries
WHERE category_code = 'edu_content'
  AND (school_period_start IS NULL OR school_period_end IS NULL OR school_period_end < school_period_start)
ON CONFLICT (entry_id, rule_code) DO NOTHING;

ALTER TABLE entries
  ADD CONSTRAINT entries_school_model_check CHECK (
    (category_code NOT IN ('it_clubs', 'teacher_training', 'edu_content')
      AND school_program_name IS NULL AND school_class_range IS NULL AND school_agreement_reference IS NULL
      AND school_groups_reference IS NULL AND school_act_reference IS NULL AND school_period_start IS NULL
      AND school_period_end IS NULL AND school_funding_source IS NULL AND school_budget_funding IS NULL
      AND school_citizen_funding IS NULL AND NOT school_incomplete)
    OR (category_code IN ('it_clubs', 'teacher_training', 'edu_content')
      AND school_incomplete = (school_program_name IS NULL)));

CREATE INDEX entries_school_agreement_idx ON entries (agreement_id, report_year, period_type)
  WHERE category_code IN ('it_clubs', 'teacher_training', 'edu_content');
