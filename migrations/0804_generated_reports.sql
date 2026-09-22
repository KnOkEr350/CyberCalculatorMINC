-- REP-01: immutable registry metadata for generated regulatory files.
CREATE TABLE generated_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    it_company_id UUID NOT NULL REFERENCES accredited_it_companies(id) ON DELETE CASCADE,
    report_type TEXT NOT NULL CHECK (report_type IN ('annex1','annex2','annex3','annex4','annex5','agreement2','agreement3','plan_fact','custom')),
    file_format TEXT NOT NULL CHECK (file_format IN ('xlsx','docx')),
    report_year INTEGER NOT NULL CHECK (report_year BETWEEN 2000 AND 2100),
    partner_id UUID REFERENCES partners(id) ON DELETE SET NULL,
    agreement_id UUID REFERENCES agreements(id) ON DELETE SET NULL,
    file_name TEXT NOT NULL CHECK (length(trim(file_name)) BETWEEN 1 AND 255),
    content_sha256 TEXT NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    content_bytes BYTEA NOT NULL,
    filters JSONB NOT NULL DEFAULT '{}'::jsonb,
    generated_by UUID NOT NULL REFERENCES users(id),
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (octet_length(content_bytes)=size_bytes)
);

CREATE INDEX generated_reports_company_time_idx ON generated_reports(it_company_id,generated_at DESC);
CREATE INDEX generated_reports_type_year_idx ON generated_reports(report_type,report_year);
