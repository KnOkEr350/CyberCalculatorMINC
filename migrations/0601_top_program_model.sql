-- TOP-01 / TOP-04 / TOP-06 / TOP-07 / TOP-11: типизированная модель ТОП-ИТ/ТОП-ИИ (Вид 4).
--
-- Паспорт программы (проект, программа, волна, роль партнёра, грант, план
-- софинансирования X) раньше жил только в payload. Теперь это типизированные
-- колонки, которые выводит триггер из проверенного payload — как в 0501 для
-- ООП/РПД: расхождения между ними быть не может, а пути записи не меняются.
--
-- Пороги 30/70% и знаменатель прогресса здесь НЕ реализуются: ADR-14 не принят,
-- а Приказ их не устанавливает. Хранятся исходные суммы (грант и X), из
-- которых показатель можно будет посчитать по утверждённому правилу.
ALTER TABLE entries
  ADD COLUMN top_project_name TEXT,
  ADD COLUMN top_program_name TEXT,
  ADD COLUMN top_wave TEXT,
  ADD COLUMN top_partner_role TEXT,
  ADD COLUMN top_grant_rub NUMERIC(20,2),
  ADD COLUMN top_planned_cofinancing_rub NUMERIC(20,2),
  -- Паспорт неполон: не названы проект или программа.
  ADD COLUMN top_incomplete BOOLEAN NOT NULL DEFAULT FALSE,
  -- Старая запись Вида 4 по колледжу или школе: программа только для ВО, такая
  -- запись сохранена, но в зачёт не допускается и лежит в находках на решение.
  ADD COLUMN top_legacy_non_vo BOOLEAN NOT NULL DEFAULT FALSE;

CREATE OR REPLACE FUNCTION entries_top_it_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  role_text TEXT; grant_text TEXT; planned_text TEXT;
BEGIN
  IF NEW.category_code <> 'top_it' THEN
    NEW.top_project_name := NULL; NEW.top_program_name := NULL; NEW.top_wave := NULL;
    NEW.top_partner_role := NULL; NEW.top_grant_rub := NULL; NEW.top_planned_cofinancing_rub := NULL;
    NEW.top_incomplete := FALSE; NEW.top_legacy_non_vo := FALSE;
    RETURN NEW;
  END IF;
  role_text := lower(btrim(COALESCE(NEW.payload->>'partner_role', '')));
  grant_text := replace(btrim(COALESCE(NEW.payload->>'grant_amount_rub', '')), ',', '.');
  planned_text := replace(btrim(COALESCE(NEW.payload->>'planned_cofinancing_amount_rub', '')), ',', '.');
  NEW.top_project_name := NULLIF(left(btrim(COALESCE(NEW.payload->>'project_name', '')), 300), '');
  NEW.top_program_name := NULLIF(left(btrim(COALESCE(NEW.payload->>'program_name', '')), 500), '');
  NEW.top_wave := NULLIF(left(btrim(COALESCE(NEW.payload->>'program_wave', '')), 100), '');
  NEW.top_partner_role := CASE WHEN role_text IN ('anchor', 'partner') THEN role_text END;
  NEW.top_grant_rub := CASE WHEN grant_text ~ '^[0-9]{1,17}(\.[0-9]{1,2})?$' THEN grant_text::numeric END;
  NEW.top_planned_cofinancing_rub := CASE WHEN planned_text ~ '^[0-9]{1,17}(\.[0-9]{1,2})?$' THEN planned_text::numeric END;
  NEW.top_incomplete := NEW.top_project_name IS NULL OR NEW.top_program_name IS NULL;
  IF TG_OP = 'INSERT' OR NEW.audience = 'vuz' THEN
    NEW.top_legacy_non_vo := FALSE;
  ELSE
    NEW.top_legacy_non_vo := OLD.top_legacy_non_vo;
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER entries_top_it_sync_trg
BEFORE INSERT OR UPDATE ON entries
FOR EACH ROW EXECUTE FUNCTION entries_top_it_sync();

-- TOP-11: перенос старых записей. Меняются только производные колонки, поэтому
-- триггер пересчёта статуса отчёта на время заполнения выключен — иначе
-- утверждённые отчёты вернулись бы в черновик.
ALTER TABLE entries DISABLE TRIGGER entries_reset_report_workflow;
UPDATE entries SET payload = payload WHERE category_code = 'top_it';
-- Отметку ставит только эта миграция, а триггер её сохраняет, но сам не выставляет.
ALTER TABLE entries DISABLE TRIGGER entries_top_it_sync_trg;
UPDATE entries SET top_legacy_non_vo = TRUE WHERE category_code = 'top_it' AND audience <> 'vuz';
ALTER TABLE entries ENABLE TRIGGER entries_top_it_sync_trg;
ALTER TABLE entries ENABLE TRIGGER entries_reset_report_workflow;

INSERT INTO legacy_backfill_findings(entry_id, category_code, rule_code, detail)
SELECT id, 'top_it', 'top.non_vo_audience',
  'Программа ТОП-ИТ/ТОП-ИИ только для высшего образования, а запись заведена для аудитории ' || audience ||
  ': запись сохранена, но в зачёт не допускается — уточните аудиторию или удалите запись'
FROM entries WHERE category_code = 'top_it' AND top_legacy_non_vo
ON CONFLICT (entry_id, rule_code) DO NOTHING;

INSERT INTO legacy_backfill_findings(entry_id, category_code, rule_code, detail)
SELECT id, 'top_it', 'top.passport_incomplete',
  'Не определено: ' || concat_ws(', ',
    CASE WHEN top_project_name IS NULL THEN 'наименование проекта (project_name)' END,
    CASE WHEN top_program_name IS NULL THEN 'наименование программы (program_name)' END)
FROM entries WHERE category_code = 'top_it' AND top_incomplete
ON CONFLICT (entry_id, rule_code) DO NOTHING;

ALTER TABLE entries
  ADD CONSTRAINT entries_top_it_model_check CHECK (
    (category_code <> 'top_it'
      AND top_project_name IS NULL AND top_program_name IS NULL AND top_wave IS NULL
      AND top_partner_role IS NULL AND top_grant_rub IS NULL AND top_planned_cofinancing_rub IS NULL
      AND NOT top_incomplete AND NOT top_legacy_non_vo)
    OR (category_code = 'top_it'
      AND top_incomplete = (top_project_name IS NULL OR top_program_name IS NULL)
      -- Только ВО: колледж и школа допустимы лишь у помеченных старых записей.
      AND (audience = 'vuz' OR top_legacy_non_vo))),
  ADD CONSTRAINT entries_top_amounts_check CHECK (
    (top_grant_rub IS NULL OR top_grant_rub >= 0) AND (top_planned_cofinancing_rub IS NULL OR top_planned_cofinancing_rub >= 0));

-- TOP-04 / TOP-06 / TOP-07: составляющие программы. Одна таблица с видом
-- строки: у каждого вида — свои обязательные поля, и БД проверяет их сама.
CREATE TABLE top_program_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('support', 'scholarship', 'case')),
  title TEXT NOT NULL CHECK (length(btrim(title)) BETWEEN 2 AND 300),
  -- Неденежная поддержка: оборудование или ПО по акту.
  support_kind TEXT CHECK (support_kind IN ('equipment', 'software')),
  act_reference TEXT CHECK (act_reference IS NULL OR length(btrim(act_reference)) BETWEEN 1 AND 500),
  act_date DATE,
  balance_value_rub NUMERIC(20,2) CHECK (balance_value_rub IS NULL OR balance_value_rub >= 0),
  appraised_value_rub NUMERIC(20,2) CHECK (appraised_value_rub IS NULL OR appraised_value_rub >= 0),
  confirmed_value_rub NUMERIC(20,2) CHECK (confirmed_value_rub IS NULL OR confirmed_value_rub >= 0),
  -- Стипендиат.
  student_name TEXT CHECK (student_name IS NULL OR length(btrim(student_name)) BETWEEN 2 AND 300),
  group_name TEXT CHECK (group_name IS NULL OR length(btrim(group_name)) BETWEEN 1 AND 100),
  course INTEGER CHECK (course IS NULL OR course BETWEEN 1 AND 6),
  period_start DATE,
  period_end DATE,
  amount_rub NUMERIC(20,2) CHECK (amount_rub IS NULL OR amount_rub > 0),
  criterion TEXT CHECK (criterion IS NULL OR length(btrim(criterion)) BETWEEN 1 AND 1000),
  donor_name TEXT CHECK (donor_name IS NULL OR length(btrim(donor_name)) BETWEEN 1 AND 300),
  -- Производственный кейс.
  implementation_org TEXT CHECK (implementation_org IS NULL OR length(btrim(implementation_org)) BETWEEN 1 AND 300),
  implementation_status TEXT CHECK (implementation_status IN ('proposed', 'implemented')),
  implemented_on DATE,
  description TEXT CHECK (description IS NULL OR length(btrim(description)) BETWEEN 1 AND 4000),
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT top_item_support_fields CHECK (kind <> 'support' OR (
    support_kind IS NOT NULL AND act_reference IS NOT NULL AND act_date IS NOT NULL
    AND (balance_value_rub IS NOT NULL OR appraised_value_rub IS NOT NULL)
    -- Подтверждённая стоимость не выше того, чем подтверждена: балансовой или оценочной.
    AND (confirmed_value_rub IS NULL OR confirmed_value_rub <= GREATEST(COALESCE(balance_value_rub, 0), COALESCE(appraised_value_rub, 0))))),
  CONSTRAINT top_item_scholarship_fields CHECK (kind <> 'scholarship' OR (
    student_name IS NOT NULL AND group_name IS NOT NULL AND course IS NOT NULL
    AND period_start IS NOT NULL AND period_end IS NOT NULL AND period_end >= period_start
    AND amount_rub IS NOT NULL AND criterion IS NOT NULL AND donor_name IS NOT NULL)),
  CONSTRAINT top_item_case_fields CHECK (kind <> 'case' OR (
    implementation_org IS NOT NULL AND implementation_status IS NOT NULL AND description IS NOT NULL
    AND ((implementation_status = 'implemented') = (implemented_on IS NOT NULL)))),
  -- Поля чужого вида не заполняются: строка не может быть сразу и стипендией, и кейсом.
  CONSTRAINT top_item_no_foreign_fields CHECK (
    (kind = 'support' OR (support_kind IS NULL AND act_reference IS NULL AND act_date IS NULL
      AND balance_value_rub IS NULL AND appraised_value_rub IS NULL AND confirmed_value_rub IS NULL))
    AND (kind = 'scholarship' OR (student_name IS NULL AND group_name IS NULL AND course IS NULL
      AND period_start IS NULL AND period_end IS NULL AND amount_rub IS NULL AND criterion IS NULL AND donor_name IS NULL))
    AND (kind = 'case' OR (implementation_org IS NULL AND implementation_status IS NULL
      AND implemented_on IS NULL AND description IS NULL)))
);
CREATE INDEX top_program_items_entry_idx ON top_program_items (entry_id, kind, created_at);

-- Подтверждающие документы строки. Размер каждого ограничен общим лимитом
-- вложений (20 МБ); здесь фиксируется только связь.
CREATE TABLE top_item_documents (
  item_id UUID NOT NULL REFERENCES top_program_items(id) ON DELETE CASCADE,
  attachment_id UUID NOT NULL REFERENCES attachments(id) ON DELETE CASCADE,
  PRIMARY KEY (item_id, attachment_id)
);

-- Строка принадлежит записи Вида 4 и ни какой другой.
CREATE OR REPLACE FUNCTION top_program_items_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE parent_category TEXT;
BEGIN
  SELECT category_code INTO parent_category FROM public.entries WHERE id = NEW.entry_id;
  IF parent_category IS DISTINCT FROM 'top_it' THEN
    RAISE EXCEPTION 'top program items belong to top_it entries only';
  END IF;
  IF TG_OP = 'UPDATE' AND (NEW.entry_id IS DISTINCT FROM OLD.entry_id OR NEW.kind IS DISTINCT FROM OLD.kind) THEN
    RAISE EXCEPTION 'top program item entry and kind are immutable';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER top_program_items_guard_trg
BEFORE INSERT OR UPDATE ON top_program_items
FOR EACH ROW EXECUTE FUNCTION top_program_items_guard();
