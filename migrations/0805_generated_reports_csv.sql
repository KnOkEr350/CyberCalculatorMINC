-- REPORT-01/REPORT-10: CSV is a first-class generated plan/fact artifact and
-- must be stored in the same immutable registry as XLSX and DOCX files.
ALTER TABLE generated_reports
    DROP CONSTRAINT generated_reports_file_format_check;

ALTER TABLE generated_reports
    ADD CONSTRAINT generated_reports_file_format_check
    CHECK (file_format IN ('xlsx','docx','csv'));
