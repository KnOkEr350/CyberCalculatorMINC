-- Regional education authorities are first-class entities. A school activity
-- is linked through entry -> ROIV agreement -> school, preserving legacy rows.

CREATE TABLE regional_authorities (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 2 AND 300),
  region TEXT NOT NULL CHECK(length(trim(region)) BETWEEN 2 AND 200),
  inn TEXT NOT NULL CHECK(length(inn) IN (10,12)),
  ogrn TEXT NOT NULL CHECK(length(ogrn) IN (13,15)),
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','inactive')),
  source_url TEXT NOT NULL,
  created_by UUID REFERENCES users(id),
  updated_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX regional_authorities_name_region_unique
  ON regional_authorities(lower(name),lower(region));
CREATE INDEX regional_authorities_region_status_idx
  ON regional_authorities(region,status);

ALTER TABLE agreements
  ADD COLUMN regional_authority_id UUID REFERENCES regional_authorities(id);
CREATE INDEX agreements_regional_authority_idx
  ON agreements(regional_authority_id,status,valid_from,valid_until);

-- Historical ROIV agreements remain readable and must be reviewed. Every new
-- or updated ROIV agreement must select a real authority; ordinary agreements
-- cannot accidentally retain an authority link.
ALTER TABLE agreements ADD CONSTRAINT agreements_regional_authority_kind
  CHECK(
    (agreement_kind='roiv' AND (status='needs_review' OR regional_authority_id IS NOT NULL))
    OR (agreement_kind='education_organization' AND regional_authority_id IS NULL)
  ) NOT VALID;

CREATE VIEW regional_authority_school_activities AS
SELECT
  ra.id AS regional_authority_id,
  ra.name AS regional_authority_name,
  ra.region,
  a.id AS agreement_id,
  a.number AS agreement_number,
  p.id AS school_partner_id,
  p.name AS school_name,
  e.id AS entry_id,
  e.category_code,
  e.period_type,
  e.report_year
FROM regional_authorities ra
JOIN agreements a ON a.regional_authority_id=ra.id AND a.agreement_kind='roiv'
JOIN agreement_partners ap ON ap.agreement_id=a.id
JOIN partners p ON p.id=ap.partner_id AND p.partner_kind='school'
LEFT JOIN entries e ON e.agreement_id=a.id AND e.partner_id=p.id;
