-- Additive migration; existing entries and files are preserved.
CREATE TABLE mentors (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 partner_id UUID NOT NULL REFERENCES partners(id),
 full_name TEXT NOT NULL CHECK(length(trim(full_name)) BETWEEN 2 AND 200),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX mentors_partner_name ON mentors(partner_id, lower(full_name));
INSERT INTO mentors(partner_id, full_name)
 SELECT DISTINCT ON (partner_id, lower(trim(payload->>'mentor_full_name')))
 partner_id, trim(payload->>'mentor_full_name') FROM entries
 WHERE partner_id IS NOT NULL AND length(trim(payload->>'mentor_full_name')) BETWEEN 2 AND 200;
UPDATE entries e SET payload = e.payload || jsonb_build_object('mentor_id', m.id::text)
 FROM mentors m WHERE e.partner_id=m.partner_id
 AND lower(trim(e.payload->>'mentor_full_name'))=lower(m.full_name);
CREATE INDEX entries_partner_period_year ON entries(partner_id, report_year, period_type);
CREATE TABLE education_directory (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 2 AND 1000),
 partner_kind TEXT NOT NULL CHECK(partner_kind IN ('vuz','kolledj','school')),
 region TEXT NOT NULL DEFAULT '', source TEXT NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(name, partner_kind, region)
);
ALTER TABLE partners ADD COLUMN directory_id UUID REFERENCES education_directory(id);
CREATE UNIQUE INDEX partners_directory_unique ON partners(directory_id) WHERE directory_id IS NOT NULL;
CREATE TABLE entry_imports (
 fingerprint TEXT PRIMARY KEY, created_by UUID NOT NULL REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
UPDATE activity_categories SET audience_scope=ARRAY['vuz'] WHERE code='top_it';
