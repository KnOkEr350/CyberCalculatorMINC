-- DATA-01 / DATA-06: a unified organization projection and a normalized,
-- versioned specialty catalog. Legacy arrays remain readable during cutover.

ALTER TABLE accredited_it_companies
  ADD COLUMN kpp TEXT NOT NULL DEFAULT '',
  ADD COLUMN organization_role TEXT NOT NULL DEFAULT 'obligated'
    CHECK (organization_role IN ('obligated','authorized','participant'));

ALTER TABLE education_directory
  ADD COLUMN kpp TEXT NOT NULL DEFAULT '',
  ADD COLUMN legal_address TEXT NOT NULL DEFAULT '',
  ADD COLUMN accreditation_number TEXT NOT NULL DEFAULT '',
  ADD COLUMN accreditation_status TEXT NOT NULL DEFAULT 'unknown'
    CHECK (accreditation_status IN ('unknown','active','suspended','expired','revoked'));

ALTER TABLE regional_authorities
  ADD COLUMN kpp TEXT NOT NULL DEFAULT '',
  ADD COLUMN legal_address TEXT NOT NULL DEFAULT '';

ALTER TABLE legal_entity_group_members
  ADD COLUMN organization_role TEXT NOT NULL DEFAULT 'participant'
    CHECK (organization_role IN ('obligated','authorized','participant'));

CREATE VIEW organization_directory_v44 AS
SELECT 'it_company'::TEXT AS source_type,id,name,'it_company'::TEXT AS organization_kind,
  inn,kpp,ogrn,legal_address,accreditation_number,accreditation_status,
  organization_role,source_url,updated_at
FROM accredited_it_companies
UNION ALL
SELECT 'education_directory',id,name,
  CASE partner_kind WHEN 'vuz' THEN 'higher_education'
    WHEN 'kolledj' THEN 'secondary_vocational' ELSE 'school' END,
  COALESCE(inn,''),kpp,COALESCE(ogrn,''),legal_address,accreditation_number,
  accreditation_status,'counterparty',COALESCE(source_url,source),updated_at
FROM education_directory
UNION ALL
SELECT 'regional_authority',id,name,'regional_authority',inn,kpp,ogrn,
  legal_address,'','not_applicable','counterparty',source_url,updated_at
FROM regional_authorities;

CREATE TABLE specialty_catalog_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  code TEXT NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9][a-z0-9._-]{2,63}$'),
  title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 3 AND 500),
  effective_from DATE NOT NULL,
  effective_until DATE CHECK (effective_until IS NULL OR effective_until>=effective_from),
  normative_source_id UUID REFERENCES normative_sources(id) ON DELETE RESTRICT,
  source_reference TEXT NOT NULL CHECK (length(trim(source_reference)) BETWEEN 3 AND 2000),
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','retired')),
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX specialty_catalog_one_open_active
  ON specialty_catalog_versions((status)) WHERE status='active' AND effective_until IS NULL;

CREATE FUNCTION reject_overlapping_specialty_catalog() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status='active' AND EXISTS(
    SELECT 1 FROM specialty_catalog_versions existing
    WHERE existing.id<>NEW.id AND existing.status='active'
      AND daterange(existing.effective_from,COALESCE(existing.effective_until,'infinity'::date),'[]')
          && daterange(NEW.effective_from,COALESCE(NEW.effective_until,'infinity'::date),'[]')
  ) THEN RAISE EXCEPTION 'active specialty catalog periods overlap'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER specialty_catalog_no_overlap
BEFORE INSERT OR UPDATE OF status,effective_from,effective_until ON specialty_catalog_versions
FOR EACH ROW EXECUTE FUNCTION reject_overlapping_specialty_catalog();

CREATE TABLE specialties (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  catalog_version_id UUID NOT NULL REFERENCES specialty_catalog_versions(id) ON DELETE RESTRICT,
  code TEXT NOT NULL CHECK (code ~ '^[0-9]{2}\.[0-9]{2}\.[0-9]{2}$'),
  education_system TEXT NOT NULL CHECK (education_system IN ('higher','secondary_vocational')),
  qualification_level TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 1000),
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  UNIQUE(catalog_version_id,code)
);
CREATE INDEX specialties_lookup_idx ON specialties(code,is_active);

CREATE TABLE organization_specialties (
  organization_id UUID NOT NULL REFERENCES education_directory(id) ON DELETE CASCADE,
  specialty_id UUID NOT NULL REFERENCES specialties(id) ON DELETE RESTRICT,
  source_url TEXT NOT NULL DEFAULT '',
  checked_at TIMESTAMPTZ,
  PRIMARY KEY(organization_id,specialty_id)
);

-- Existing Order 27 data receives an explicit compatibility version. It is
-- intentionally marked active but keeps its source reference visible until a
-- BASE-13 source is linked by a later catalog import.
INSERT INTO specialty_catalog_versions(code,title,effective_from,source_reference,status)
VALUES('order-27-2026.legacy','Приказ Минцифры России № 27: импортированный перечень',DATE '2026-01-22',
  'Миграция 0022; требуется связать с проверенной редакцией BASE-13','active');

INSERT INTO specialties(catalog_version_id,code,education_system,title)
SELECT version.id,imported.code,'higher',imported.code
FROM specialty_catalog_versions version
CROSS JOIN LATERAL (
  SELECT DISTINCT unnest(program_codes) AS code FROM education_directory
) imported
WHERE version.code='order-27-2026.legacy'
ON CONFLICT DO NOTHING;

INSERT INTO organization_specialties(organization_id,specialty_id,source_url,checked_at)
SELECT directory.id,specialty.id,directory.programs_source_url,directory.programs_checked_at
FROM education_directory directory
JOIN specialty_catalog_versions version ON version.code='order-27-2026.legacy'
JOIN specialties specialty ON specialty.catalog_version_id=version.id
  AND specialty.code=ANY(directory.program_codes)
ON CONFLICT DO NOTHING;

CREATE VIEW active_specialties AS
SELECT specialty.id,specialty.code,specialty.education_system,
  specialty.qualification_level,specialty.title,version.code AS catalog_version,
  version.effective_from,version.source_reference,version.normative_source_id
FROM specialties specialty
JOIN specialty_catalog_versions version ON version.id=specialty.catalog_version_id
WHERE specialty.is_active AND version.status='active'
  AND version.effective_from<=CURRENT_DATE
  AND (version.effective_until IS NULL OR version.effective_until>=CURRENT_DATE);
