-- Verified educational institutions and first-class agreements.
-- The migration is additive: legacy partners and entries remain available,
-- while all new entries must reference an agreement covering their partner.

ALTER TABLE education_directory
  ADD COLUMN inn TEXT,
  ADD COLUMN ogrn TEXT,
  ADD COLUMN license_number TEXT,
  ADD COLUMN license_status TEXT NOT NULL DEFAULT 'unknown'
    CHECK (license_status IN ('active','suspended','expired','revoked','unknown')),
  ADD COLUMN institution_status TEXT NOT NULL DEFAULT 'unknown'
    CHECK (institution_status IN ('active','inactive','reorganized','liquidated','unknown')),
  ADD COLUMN registry_record_id TEXT,
  ADD COLUMN source_url TEXT,
  ADD COLUMN registry_updated_at DATE,
  ADD COLUMN verified_at TIMESTAMPTZ,
  ADD COLUMN verification_status TEXT NOT NULL DEFAULT 'monitoring_only'
    CHECK (verification_status IN ('verified','monitoring_only','pending','rejected'));

CREATE UNIQUE INDEX education_directory_registry_record_unique
  ON education_directory(registry_record_id)
  WHERE registry_record_id IS NOT NULL AND registry_record_id <> '';
CREATE INDEX education_directory_inn_idx ON education_directory(inn);
CREATE INDEX education_directory_verification_idx
  ON education_directory(partner_kind, verification_status, license_status, institution_status);

CREATE TABLE directory_sync_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_url TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('started','completed','failed')),
  imported_count INTEGER NOT NULL DEFAULT 0 CHECK(imported_count >= 0),
  rejected_count INTEGER NOT NULL DEFAULT 0 CHECK(rejected_count >= 0),
  error_text TEXT,
  started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at TIMESTAMPTZ
);

CREATE TABLE agreements (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agreement_kind TEXT NOT NULL CHECK(agreement_kind IN ('education_organization','roiv')),
  number TEXT NOT NULL CHECK(length(trim(number)) BETWEEN 1 AND 100),
  status TEXT NOT NULL CHECK(status IN ('needs_review','draft','active','suspended','expired','terminated')),
  signed_on DATE NOT NULL,
  valid_from DATE NOT NULL,
  valid_until DATE NOT NULL,
  roiv_name TEXT,
  legal_entity_group TEXT,
  signature_method TEXT NOT NULL DEFAULT 'unsigned'
    CHECK(signature_method IN ('unsigned','paper','qualified_electronic','goskey')),
  signed_by TEXT,
  signature_date DATE,
  document_reference TEXT,
  notes TEXT,
  created_by UUID REFERENCES users(id),
  updated_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK(valid_until >= valid_from),
  CHECK(agreement_kind <> 'roiv' OR length(trim(COALESCE(roiv_name,''))) > 0),
  CHECK(status <> 'active' OR (
    signature_method <> 'unsigned' AND
    length(trim(COALESCE(signed_by,''))) > 0 AND
    signature_date IS NOT NULL
  ))
);
CREATE INDEX agreements_period_status_idx ON agreements(status, valid_from, valid_until);
CREATE INDEX agreements_number_idx ON agreements(lower(number));

CREATE TABLE agreement_partners (
  agreement_id UUID NOT NULL REFERENCES agreements(id),
  partner_id UUID NOT NULL REFERENCES partners(id),
  is_primary BOOLEAN NOT NULL DEFAULT FALSE,
  PRIMARY KEY(agreement_id, partner_id)
);
CREATE INDEX agreement_partners_partner_idx ON agreement_partners(partner_id, agreement_id);
CREATE UNIQUE INDEX agreement_one_primary_partner_idx
  ON agreement_partners(agreement_id) WHERE is_primary;

CREATE TABLE agreement_responsible_people (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
  party TEXT NOT NULL CHECK(party IN ('cyberprotect','counterparty')),
  full_name TEXT NOT NULL CHECK(length(trim(full_name)) BETWEEN 2 AND 200),
  position TEXT CHECK(length(COALESCE(position,'')) <= 200),
  email TEXT CHECK(length(COALESCE(email,'')) <= 254),
  phone TEXT CHECK(length(COALESCE(phone,'')) <= 40),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX agreement_people_agreement_idx ON agreement_responsible_people(agreement_id, party);

-- Each legacy partner receives a clearly marked agreement requiring review.
-- Existing structured details are retained; absent legacy details are replaced
-- only by an explicit technical number/date so no record is lost.
CREATE TEMP TABLE legacy_agreement_map AS
SELECT id AS partner_id, gen_random_uuid() AS agreement_id FROM partners;

INSERT INTO agreements(
  id, agreement_kind, number, status, signed_on, valid_from, valid_until,
  roiv_name, signature_method, notes, created_at, updated_at
)
SELECT
  map.agreement_id,
  CASE WHEN p.partner_kind='school' THEN 'roiv' ELSE 'education_organization' END,
  COALESCE(NULLIF(trim(p.agreement_number),''), 'LEGACY-' || left(p.id::text,8)),
  'needs_review',
  COALESCE(p.agreement_date, p.created_at::date),
  COALESCE(p.agreement_date, p.created_at::date),
  DATE '9999-12-31',
  CASE WHEN p.partner_kind='school' THEN 'Требует уточнения' END,
  'unsigned',
  concat_ws(E'\n', 'Перенесено из прежней модели; реквизиты и статус необходимо проверить.', NULLIF(trim(p.other_agreement),'')),
  p.created_at,
  p.updated_at
FROM partners p JOIN legacy_agreement_map map ON map.partner_id=p.id;

INSERT INTO agreement_partners(agreement_id, partner_id, is_primary)
SELECT map.agreement_id, map.partner_id, TRUE
FROM legacy_agreement_map map;

ALTER TABLE entries ADD COLUMN agreement_id UUID REFERENCES agreements(id);
UPDATE entries e SET agreement_id = ap.agreement_id
FROM agreement_partners ap
WHERE ap.partner_id=e.partner_id AND ap.is_primary;
CREATE INDEX entries_agreement_idx ON entries(agreement_id);

-- NOT VALID keeps any historical orphaned rows readable, but PostgreSQL still
-- enforces both constraints for every new or modified row.
ALTER TABLE entries ADD CONSTRAINT entries_agreement_required
  CHECK(agreement_id IS NOT NULL) NOT VALID;
ALTER TABLE entries ADD CONSTRAINT entries_agreement_covers_partner
  FOREIGN KEY(agreement_id, partner_id)
  REFERENCES agreement_partners(agreement_id, partner_id) NOT VALID;

-- Mandatory activities are evaluated inside the selected agreement. An
-- activity from another agreement of the same partner cannot accidentally
-- satisfy its prerequisites.
CREATE OR REPLACE VIEW entry_eligibility AS
WITH activity_status AS (
  SELECT partner_id,agreement_id,report_year,period_type,
    bool_or(category_code='teachers' AND amount_rub>0) AS teachers,
    bool_or(category_code='ood_rpd' AND amount_rub>0) AS programs,
    bool_or(category_code='top_it' AND amount_rub>0) AS top_it,
    bool_or(category_code<>'top_it' AND amount_rub>0) AS other_activity
  FROM entries GROUP BY partner_id,agreement_id,report_year,period_type
)
SELECT e.id,e.partner_id,e.report_year,e.period_type,e.amount_rub,
  CASE
    WHEN p.id IS NULL THEN false
    WHEN p.partner_kind<>'vuz' THEN true
    WHEN e.category_code IN ('teachers','ood_rpd') THEN true
    WHEN s.top_it THEN EXISTS(SELECT 1 FROM activity_status other
      WHERE other.partner_id<>e.partner_id AND other.report_year=e.report_year
        AND other.period_type=e.period_type AND other.other_activity)
    ELSE s.teachers AND s.programs
  END AS eligible
FROM entries e LEFT JOIN partners p ON p.id=e.partner_id
LEFT JOIN activity_status s ON s.partner_id=e.partner_id AND s.agreement_id IS NOT DISTINCT FROM e.agreement_id
  AND s.report_year=e.report_year AND s.period_type=e.period_type;

DROP TABLE legacy_agreement_map;
