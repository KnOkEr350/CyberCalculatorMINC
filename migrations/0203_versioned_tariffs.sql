-- DATA-07: versioned tariff/formula inputs. Production calculations select a
-- version by report year and persist the selected version on every entry.

CREATE TABLE tariff_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  code TEXT NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9][a-z0-9._-]{2,63}$'),
  title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 3 AND 500),
  effective_from DATE NOT NULL,
  effective_until DATE CHECK (effective_until IS NULL OR effective_until>=effective_from),
  calculation_mode TEXT NOT NULL DEFAULT 'average' CHECK (calculation_mode IN ('average','actual')),
  normative_source_id UUID REFERENCES normative_sources(id) ON DELETE RESTRICT,
  source_reference TEXT NOT NULL CHECK (length(trim(source_reference)) BETWEEN 3 AND 2000),
  provenance_verified BOOLEAN NOT NULL DEFAULT FALSE,
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','retired')),
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (NOT provenance_verified OR normative_source_id IS NOT NULL)
);
CREATE INDEX tariff_versions_effective_idx
  ON tariff_versions(calculation_mode,status,effective_from DESC);

CREATE FUNCTION reject_overlapping_tariff_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.status='active' AND EXISTS(
    SELECT 1 FROM tariff_versions existing
    WHERE existing.id<>NEW.id AND existing.status='active'
      AND existing.calculation_mode=NEW.calculation_mode
      AND daterange(existing.effective_from,COALESCE(existing.effective_until,'infinity'::date),'[]')
          && daterange(NEW.effective_from,COALESCE(NEW.effective_until,'infinity'::date),'[]')
  ) THEN RAISE EXCEPTION 'active tariff periods overlap'; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER tariff_versions_no_overlap
BEFORE INSERT OR UPDATE OF status,effective_from,effective_until,calculation_mode ON tariff_versions
FOR EACH ROW EXECUTE FUNCTION reject_overlapping_tariff_version();

CREATE TABLE tariff_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tariff_version_id UUID NOT NULL REFERENCES tariff_versions(id) ON DELETE CASCADE,
  activity_code TEXT NOT NULL REFERENCES activity_categories(code),
  audience TEXT NOT NULL CHECK (audience IN ('vuz','kolledj','school','all')),
  component_code TEXT NOT NULL CHECK (component_code ~ '^[a-z0-9][a-z0-9._-]{1,127}$'),
  rate_rub NUMERIC(16,2) NOT NULL CHECK (rate_rub>=0),
  unit_code TEXT NOT NULL CHECK (length(trim(unit_code)) BETWEEN 1 AND 100),
  UNIQUE(tariff_version_id,activity_code,audience,component_code)
);
CREATE INDEX tariff_rules_lookup_idx
  ON tariff_rules(activity_code,audience,tariff_version_id);

INSERT INTO tariff_versions(code,title,effective_from,source_reference,status,provenance_verified)
VALUES('order-270-2026.legacy','Тарифы приложения № 6 приказа № 270',DATE '2026-01-01',
  'Локальная редакция ТЗ; до связи с BASE-13 отображается как непроверенный provenance','active',FALSE);

INSERT INTO tariff_rules(tariff_version_id,activity_code,audience,component_code,rate_rub,unit_code)
SELECT version.id,rule.activity_code,rule.audience,rule.component_code,rule.rate_rub,rule.unit_code
FROM tariff_versions version
CROSS JOIN (VALUES
  ('teachers','vuz','academic_hour',4140.00,'academic_hour'),
  ('teachers','kolledj','academic_hour',3900.00,'academic_hour'),
  ('internship','all','student_hour',800.00,'person_hour'),
  ('internship','all','mentor_hour',2390.00,'person_hour'),
  ('employment_practice','all','student_hour',800.00,'person_hour'),
  ('employment_practice','all','mentor_hour',2390.00,'person_hour'),
  ('ood_rpd','vuz','rpd.vo.development',300000.00,'document'),
  ('ood_rpd','vuz','rpd.vo.update',160000.00,'document'),
  ('ood_rpd','vuz','rpd.vo.expertise',55000.00,'document'),
  ('ood_rpd','kolledj','rpd.spo.development',270750.00,'document'),
  ('ood_rpd','kolledj','rpd.spo.update',150000.00,'document'),
  ('ood_rpd','kolledj','rpd.spo.expertise',58060.00,'document'),
  ('ood_rpd','vuz','oop.vo.development',2039850.00,'document'),
  ('ood_rpd','vuz','oop.vo.update',626110.00,'document'),
  ('ood_rpd','vuz','oop.vo.expertise',312300.00,'document'),
  ('ood_rpd','kolledj','oop.spo.development',1731360.00,'document'),
  ('ood_rpd','kolledj','oop.spo.update',427440.00,'document'),
  ('ood_rpd','kolledj','oop.spo.expertise',171000.00,'document'),
  ('it_clubs','school','academic_hour',4260.00,'academic_hour'),
  ('it_clubs','school','developed_program',530890.00,'program'),
  ('teacher_training','school','academic_hour_per_teacher',3790.00,'person_hour'),
  ('teacher_training','school','developed_program',1408570.00,'program'),
  ('edu_content','school','student_platform_month',6800.00,'person_month'),
  ('edu_content','school','teacher_platform_month',8590.00,'person_month')
) AS rule(activity_code,audience,component_code,rate_rub,unit_code)
WHERE version.code='order-270-2026.legacy';

ALTER TABLE entries ADD COLUMN tariff_version_id UUID REFERENCES tariff_versions(id) ON DELETE RESTRICT;
