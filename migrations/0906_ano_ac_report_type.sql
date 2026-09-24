-- REPORT-09: отчёт для АНО АЦ по программе ТОП-ИТ/ТОП-ИИ (шесть листов по
-- ТЗ 4.4 §9.3) регистрируется в реестре сформированных файлов наравне с
-- остальными формами.
ALTER TABLE generated_reports DROP CONSTRAINT generated_reports_report_type_check;
ALTER TABLE generated_reports ADD CONSTRAINT generated_reports_report_type_check
  CHECK (report_type IN ('annex1','annex2','annex3','annex4','annex5','agreement2','agreement3','plan_fact','custom','ano_ac'));
