-- Compliance additions for Order 270 and strict IT-company tenant ownership.
-- Legacy rows are assigned from their creator where possible. If the registry
-- contains exactly one IT company, it is the safe fallback for old data.

ALTER TABLE accredited_it_companies
  ADD COLUMN legal_address TEXT NOT NULL DEFAULT '',
  ADD COLUMN phone TEXT NOT NULL DEFAULT '',
  ADD COLUMN email TEXT NOT NULL DEFAULT '',
  ADD COLUMN website TEXT NOT NULL DEFAULT '',
  ADD COLUMN director_name TEXT NOT NULL DEFAULT '';

ALTER TABLE partners ADD COLUMN it_company_id UUID REFERENCES accredited_it_companies(id);
ALTER TABLE agreements ADD COLUMN it_company_id UUID REFERENCES accredited_it_companies(id);
ALTER TABLE entries ADD COLUMN it_company_id UUID REFERENCES accredited_it_companies(id);

UPDATE agreements a SET it_company_id=u.it_company_id
FROM users u WHERE u.id=COALESCE(a.updated_by,a.created_by) AND u.it_company_id IS NOT NULL;

UPDATE partners p SET it_company_id=owned.it_company_id
FROM (
  SELECT DISTINCT ON(ap.partner_id) ap.partner_id,a.it_company_id
  FROM agreement_partners ap JOIN agreements a ON a.id=ap.agreement_id
  WHERE a.it_company_id IS NOT NULL
  ORDER BY ap.partner_id,a.updated_at DESC,a.id
) owned WHERE owned.partner_id=p.id;

UPDATE entries e SET it_company_id=COALESCE(a.it_company_id,p.it_company_id)
FROM agreements a,partners p
WHERE a.id=e.agreement_id AND p.id=e.partner_id;

UPDATE agreements SET it_company_id=(SELECT id FROM accredited_it_companies ORDER BY id LIMIT 1)
WHERE it_company_id IS NULL AND (SELECT count(*) FROM accredited_it_companies)=1;
UPDATE partners SET it_company_id=(SELECT id FROM accredited_it_companies ORDER BY id LIMIT 1)
WHERE it_company_id IS NULL AND (SELECT count(*) FROM accredited_it_companies)=1;
UPDATE entries SET it_company_id=(SELECT id FROM accredited_it_companies ORDER BY id LIMIT 1)
WHERE it_company_id IS NULL AND (SELECT count(*) FROM accredited_it_companies)=1;

DROP INDEX IF EXISTS partners_directory_unique;
CREATE UNIQUE INDEX partners_company_directory_unique
  ON partners(it_company_id,directory_id) NULLS NOT DISTINCT
  WHERE directory_id IS NOT NULL;
CREATE INDEX partners_it_company_idx ON partners(it_company_id,name);
CREATE INDEX agreements_it_company_idx ON agreements(it_company_id,valid_from,valid_until);
CREATE INDEX entries_it_company_period_idx ON entries(it_company_id,report_year,period_type);

ALTER TABLE organization_budget_targets ADD COLUMN it_company_id UUID REFERENCES accredited_it_companies(id);
UPDATE organization_budget_targets target SET it_company_id=u.it_company_id
FROM users u WHERE u.id=target.updated_by AND u.it_company_id IS NOT NULL;
UPDATE organization_budget_targets SET it_company_id=(SELECT id FROM accredited_it_companies ORDER BY id LIMIT 1)
WHERE it_company_id IS NULL AND (SELECT count(*) FROM accredited_it_companies)=1;
ALTER TABLE organization_budget_targets DROP CONSTRAINT organization_budget_targets_pkey;
ALTER TABLE organization_budget_targets ADD CONSTRAINT organization_budget_targets_company_year_key
  UNIQUE NULLS NOT DISTINCT(report_year,it_company_id);

-- Both values are retained: formula_amount_rub is the reproducible result of
-- the Ministry methodology, amount_rub is the amount included in the report.
ALTER TABLE entries
  ADD COLUMN cost_method TEXT NOT NULL DEFAULT 'average'
    CHECK(cost_method IN ('average','actual')),
  ADD COLUMN formula_amount_rub NUMERIC(16,2),
  ADD COLUMN actual_amount_rub NUMERIC(16,2)
    CHECK(actual_amount_rub IS NULL OR actual_amount_rub>0);
UPDATE entries SET formula_amount_rub=amount_rub WHERE formula_amount_rub IS NULL;
ALTER TABLE entries ALTER COLUMN formula_amount_rub SET NOT NULL;
ALTER TABLE entries ADD CONSTRAINT entries_cost_method_values_check CHECK(
  (cost_method='average' AND actual_amount_rub IS NULL AND amount_rub=formula_amount_rub)
  OR (cost_method='actual' AND period_type='fact' AND actual_amount_rub IS NOT NULL AND amount_rub=actual_amount_rub)
);

ALTER TABLE agreement_reports
  ADD COLUMN auditor_report_reference TEXT,
  ADD COLUMN actual_costs_confirmed BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE legal_entity_groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  it_company_id UUID NOT NULL REFERENCES accredited_it_companies(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 2 AND 500),
  interaction_agreement_number TEXT NOT NULL CHECK(length(trim(interaction_agreement_number)) BETWEEN 1 AND 100),
  interaction_agreement_date DATE NOT NULL,
  authorized_entity_name TEXT NOT NULL CHECK(length(trim(authorized_entity_name)) BETWEEN 2 AND 1000),
  authorized_entity_inn TEXT NOT NULL,
  authorized_entity_ogrn TEXT NOT NULL,
  created_by UUID NOT NULL REFERENCES users(id),
  updated_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(it_company_id,name),
  UNIQUE(it_company_id)
);

CREATE TABLE legal_entity_group_members (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id UUID NOT NULL REFERENCES legal_entity_groups(id) ON DELETE CASCADE,
  name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 2 AND 1000),
  inn TEXT NOT NULL,
  ogrn TEXT NOT NULL,
  is_it_organization BOOLEAN NOT NULL DEFAULT TRUE,
  target_amount_rub NUMERIC(16,2) CHECK(target_amount_rub IS NULL OR target_amount_rub>0),
  UNIQUE(group_id,inn)
);
CREATE INDEX legal_entity_groups_company_idx ON legal_entity_groups(it_company_id,name);
CREATE INDEX legal_entity_group_members_group_idx ON legal_entity_group_members(group_id,name);

ALTER TABLE agreements ADD COLUMN legal_entity_group_id UUID REFERENCES legal_entity_groups(id);

-- Prevent accidental cross-company relations even when a write path bypasses
-- the HTTP handlers (imports and maintenance scripts included).
CREATE OR REPLACE FUNCTION enforce_entry_tenant() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE agreement_company UUID; partner_company UUID;
BEGIN
  SELECT it_company_id INTO agreement_company FROM agreements WHERE id=NEW.agreement_id;
  SELECT it_company_id INTO partner_company FROM partners WHERE id=NEW.partner_id;
  IF NEW.it_company_id IS NULL OR agreement_company IS DISTINCT FROM NEW.it_company_id
     OR partner_company IS DISTINCT FROM NEW.it_company_id THEN
    RAISE EXCEPTION 'entry tenant does not match agreement and partner';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER entries_enforce_tenant
BEFORE INSERT OR UPDATE OF it_company_id,agreement_id,partner_id ON entries
FOR EACH ROW EXECUTE FUNCTION enforce_entry_tenant();

CREATE OR REPLACE FUNCTION enforce_agreement_group_tenant() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE group_company UUID;
BEGIN
  IF NEW.legal_entity_group_id IS NOT NULL THEN
    SELECT it_company_id INTO group_company FROM legal_entity_groups WHERE id=NEW.legal_entity_group_id;
    IF group_company IS DISTINCT FROM NEW.it_company_id THEN
      RAISE EXCEPTION 'agreement group belongs to another IT company';
    END IF;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER agreements_enforce_group_tenant
BEFORE INSERT OR UPDATE OF it_company_id,legal_entity_group_id ON agreements
FOR EACH ROW EXECUTE FUNCTION enforce_agreement_group_tenant();

CREATE OR REPLACE FUNCTION enforce_agreement_partner_tenant() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE agreement_company UUID; partner_company UUID;
BEGIN
  SELECT it_company_id INTO agreement_company FROM agreements WHERE id=NEW.agreement_id;
  SELECT it_company_id INTO partner_company FROM partners WHERE id=NEW.partner_id;
  IF agreement_company IS NULL OR partner_company IS DISTINCT FROM agreement_company THEN
    RAISE EXCEPTION 'agreement and partner belong to different IT companies';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER agreement_partners_enforce_tenant
BEFORE INSERT OR UPDATE OF agreement_id,partner_id ON agreement_partners
FOR EACH ROW EXECUTE FUNCTION enforce_agreement_partner_tenant();

CREATE OR REPLACE FUNCTION reset_auditor_confirmation_on_entry_change()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target_agreement UUID; target_year INTEGER; target_period TEXT;
BEGIN
  IF TG_OP='DELETE' THEN
    target_agreement:=OLD.agreement_id; target_year:=OLD.report_year; target_period:=OLD.period_type;
  ELSE
    target_agreement:=NEW.agreement_id; target_year:=NEW.report_year; target_period:=NEW.period_type;
  END IF;
  UPDATE agreement_reports SET actual_costs_confirmed=FALSE,auditor_report_reference=NULL
  WHERE agreement_id=target_agreement AND report_year=target_year AND period_type=target_period;
  IF TG_OP='DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER entries_reset_auditor_confirmation
AFTER INSERT OR UPDATE OR DELETE ON entries
FOR EACH ROW EXECUTE FUNCTION reset_auditor_confirmation_on_entry_change();
