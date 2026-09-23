-- OOP-01 / OOP-06: типизированная модель «ООП и РПД» (Вид 3).
--
-- Раньше документ, действие, программа и эксперт жили только в payload, и
-- матрица агрегации (2 документа × 3 действия) вычислялась разбором JSON. Теперь
-- они — типизированные колонки с ограничениями. Источник значений остаётся
-- прежним, проверенным на входе: колонки выводит триггер из payload, поэтому
-- расхождения между ними быть не может, а ни один путь записи не нужно менять.
ALTER TABLE entries
  ADD COLUMN oop_doc_type TEXT,
  ADD COLUMN oop_level TEXT,
  ADD COLUMN oop_activity TEXT,
  ADD COLUMN oop_program_name TEXT,
  ADD COLUMN oop_expert_name TEXT,
  ADD COLUMN oop_specialty_code TEXT,
  -- Состав сведений неполон (не определён документ, уровень, действие или
  -- программа). Полноту требует вход в систему; старые записи с пробелами
  -- лежат в legacy_backfill_findings на ручное уточнение. Значение выводит
  -- триггер.
  ADD COLUMN oop_incomplete BOOLEAN NOT NULL DEFAULT FALSE;

CREATE OR REPLACE FUNCTION entries_oop_rpd_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  doc_type TEXT; level TEXT; activity TEXT;
BEGIN
  IF NEW.category_code <> 'ood_rpd' THEN
    NEW.oop_doc_type := NULL; NEW.oop_level := NULL; NEW.oop_activity := NULL;
    NEW.oop_program_name := NULL; NEW.oop_expert_name := NULL; NEW.oop_specialty_code := NULL;
    NEW.oop_incomplete := FALSE;
    RETURN NEW;
  END IF;
  -- Допустимое значение или NULL: недопустимое не приводится «по возможности».
  doc_type := lower(btrim(COALESCE(NEW.payload->>'doc_type', '')));
  level := lower(btrim(COALESCE(NEW.payload->>'level', '')));
  activity := lower(btrim(COALESCE(NEW.payload->>'activity_type', '')));
  NEW.oop_doc_type := CASE WHEN doc_type IN ('rpd', 'oop') THEN doc_type END;
  NEW.oop_level := CASE WHEN level IN ('vo', 'spo') THEN level END;
  NEW.oop_activity := CASE WHEN activity IN ('development', 'update', 'expertise') THEN activity END;
  NEW.oop_program_name := NULLIF(btrim(COALESCE(NEW.payload->>'program_name', '')), '');
  NEW.oop_expert_name := NULLIF(btrim(COALESCE(NEW.payload->>'expert_full_name', '')), '');
  NEW.oop_specialty_code := NULLIF(btrim(COALESCE(NEW.payload->>'specialty_code', '')), '');
  NEW.oop_incomplete := NEW.oop_doc_type IS NULL OR NEW.oop_level IS NULL OR NEW.oop_activity IS NULL
    OR NEW.oop_program_name IS NULL;
  RETURN NEW;
END $$;

CREATE TRIGGER entries_oop_rpd_sync_trg
BEFORE INSERT OR UPDATE ON entries
FOR EACH ROW EXECUTE FUNCTION entries_oop_rpd_sync();

-- Заполнение старых записей. Триггер отчётного процесса пересчитывает статус
-- отчёта при любом изменении записи; здесь меняются только производные
-- колонки, основание отчёта остаётся прежним, поэтому на время заполнения
-- триггер выключен — иначе утверждённые отчёты вернулись бы в черновик.
ALTER TABLE entries DISABLE TRIGGER entries_reset_report_workflow;
UPDATE entries SET payload = payload WHERE category_code = 'ood_rpd';
ALTER TABLE entries ENABLE TRIGGER entries_reset_report_workflow;

-- Отчёт о том, что не удалось перенести: недостающее не выдумывается.
INSERT INTO legacy_backfill_findings(entry_id, category_code, rule_code, detail)
SELECT e.id, 'ood_rpd', 'oop.model_incomplete',
  'Не определено или недопустимо: ' || concat_ws(', ',
    CASE WHEN e.oop_doc_type IS NULL THEN 'вид документа (doc_type)' END,
    CASE WHEN e.oop_level IS NULL THEN 'уровень образования (level)' END,
    CASE WHEN e.oop_activity IS NULL THEN 'вид активности (activity_type)' END,
    CASE WHEN e.oop_program_name IS NULL THEN 'наименование РПД/ООП (program_name)' END)
FROM entries e WHERE e.category_code = 'ood_rpd' AND e.oop_incomplete
ON CONFLICT (entry_id, rule_code) DO NOTHING;

-- Уровень образования обязан соответствовать аудитории: ВО — вуз, СПО — колледж.
-- Расхождение в старых данных не исправляется, а помечается.
INSERT INTO legacy_backfill_findings(entry_id, category_code, rule_code, detail)
SELECT e.id, 'ood_rpd', 'oop.level_audience_mismatch',
  'Уровень образования ' || e.oop_level || ' не соответствует аудитории ' || e.audience
FROM entries e
WHERE e.category_code = 'ood_rpd' AND e.oop_level IS NOT NULL
  AND ((e.audience = 'vuz' AND e.oop_level <> 'vo') OR (e.audience = 'kolledj' AND e.oop_level <> 'spo'))
ON CONFLICT (entry_id, rule_code) DO NOTHING;

ALTER TABLE entries
  ADD CONSTRAINT entries_oop_rpd_model_check CHECK (
    (category_code <> 'ood_rpd'
      AND oop_doc_type IS NULL AND oop_level IS NULL AND oop_activity IS NULL
      AND oop_program_name IS NULL AND oop_expert_name IS NULL AND oop_specialty_code IS NULL
      AND NOT oop_incomplete)
    OR (category_code = 'ood_rpd'
      AND oop_incomplete = (oop_doc_type IS NULL OR oop_level IS NULL OR oop_activity IS NULL OR oop_program_name IS NULL))),
  ADD CONSTRAINT entries_oop_program_length CHECK (oop_program_name IS NULL OR length(oop_program_name) <= 500);

-- Матрица 2×3 читается по типизированным колонкам.
CREATE INDEX entries_oop_matrix_idx ON entries (partner_id, report_year, period_type, oop_doc_type, oop_activity)
  WHERE category_code = 'ood_rpd';
