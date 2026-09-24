-- TOP-08: реестр результатов интеллектуальной деятельности (РИД) программы
-- ТОП-ИТ/ТОП-ИИ — четвёртый вид строки рядом с поддержкой, стипендиатами и
-- кейсами (ТЗ 4.4 §9.3, лист 6): тип (ПО, модель ИИ, набор данных), состав
-- авторов и структура долей исключительных прав вуза и компании. Доли в сумме
-- дают ровно 100%; подтверждающие документы привязываются так же, как у
-- остальных строк, через top_item_documents.
ALTER TABLE top_program_items
  ADD COLUMN rid_type TEXT CHECK (rid_type IN ('software', 'ai_model', 'dataset')),
  ADD COLUMN authors TEXT CHECK (authors IS NULL OR length(btrim(authors)) BETWEEN 2 AND 2000),
  ADD COLUMN university_share_pct NUMERIC(5,2) CHECK (university_share_pct IS NULL OR university_share_pct BETWEEN 0 AND 100),
  ADD COLUMN company_share_pct NUMERIC(5,2) CHECK (company_share_pct IS NULL OR company_share_pct BETWEEN 0 AND 100);

ALTER TABLE top_program_items DROP CONSTRAINT top_program_items_kind_check;
ALTER TABLE top_program_items ADD CONSTRAINT top_program_items_kind_check
  CHECK (kind IN ('support', 'scholarship', 'case', 'rid'));

ALTER TABLE top_program_items ADD CONSTRAINT top_item_rid_fields CHECK (kind <> 'rid' OR (
  rid_type IS NOT NULL AND authors IS NOT NULL
  AND university_share_pct IS NOT NULL AND company_share_pct IS NOT NULL
  AND university_share_pct + company_share_pct = 100));

ALTER TABLE top_program_items DROP CONSTRAINT top_item_no_foreign_fields;
ALTER TABLE top_program_items ADD CONSTRAINT top_item_no_foreign_fields CHECK (
  (kind = 'support' OR (support_kind IS NULL AND act_reference IS NULL AND act_date IS NULL
    AND balance_value_rub IS NULL AND appraised_value_rub IS NULL AND confirmed_value_rub IS NULL))
  AND (kind = 'scholarship' OR (student_name IS NULL AND group_name IS NULL AND course IS NULL
    AND period_start IS NULL AND period_end IS NULL AND amount_rub IS NULL AND criterion IS NULL AND donor_name IS NULL))
  AND (kind = 'case' OR (implementation_org IS NULL AND implementation_status IS NULL
    AND implemented_on IS NULL AND description IS NULL))
  AND (kind = 'rid' OR (rid_type IS NULL AND authors IS NULL AND university_share_pct IS NULL AND company_share_pct IS NULL)));
