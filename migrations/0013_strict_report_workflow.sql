-- Strict agreement-level report workflow. An amount is eligible only after
-- the whole agreement/year/period package has passed legal checks and approval.

CREATE TABLE agreement_activity_requirements (
  agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
  category_code TEXT NOT NULL REFERENCES activity_categories(code),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(agreement_id, category_code)
);

-- Every agreement starts with every activity type applicable to at least one
-- of its educational institutions. This also gives legacy agreements an
-- explicit, reviewable scope instead of inferring it from entered rows.
INSERT INTO agreement_activity_requirements(agreement_id, category_code)
SELECT DISTINCT ap.agreement_id, c.code
FROM agreement_partners ap
JOIN partners p ON p.id=ap.partner_id
JOIN activity_categories c ON p.partner_kind=ANY(c.audience_scope)
ON CONFLICT DO NOTHING;

CREATE TABLE agreement_reports (
  agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
  report_year INTEGER NOT NULL CHECK(report_year BETWEEN 2000 AND 2100),
  period_type TEXT NOT NULL CHECK(period_type IN ('plan','fact')),
  status TEXT NOT NULL DEFAULT 'draft'
    CHECK(status IN ('draft','ready','verified','approved')),
  scope_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
  conditions_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
  evidence_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
  counterparty_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
  comment TEXT,
  ready_by UUID REFERENCES users(id),
  ready_at TIMESTAMPTZ,
  verified_by UUID REFERENCES users(id),
  verified_at TIMESTAMPTZ,
  approved_by UUID REFERENCES users(id),
  approved_at TIMESTAMPTZ,
  updated_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(agreement_id, report_year, period_type)
);
CREATE INDEX agreement_reports_status_idx
  ON agreement_reports(report_year, period_type, status);

CREATE TABLE agreement_report_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
  report_year INTEGER NOT NULL,
  period_type TEXT NOT NULL,
  from_status TEXT NOT NULL,
  to_status TEXT NOT NULL,
  comment TEXT,
  changed_by UUID NOT NULL REFERENCES users(id),
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX agreement_report_history_context_idx
  ON agreement_report_history(agreement_id, report_year, period_type, changed_at DESC);

-- Any material change invalidates prior review/approval. This applies to every
-- write path (manual input, Excel import and future maintenance scripts).
CREATE OR REPLACE FUNCTION reset_agreement_report_on_entry_change()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_agreement UUID; old_year INTEGER; old_period TEXT;
DECLARE new_agreement UUID; new_year INTEGER; new_period TEXT;
DECLARE actor UUID;
BEGIN
  IF TG_OP='DELETE' THEN actor:=COALESCE(OLD.updated_by,OLD.created_by);
  ELSE actor:=COALESCE(NEW.updated_by,NEW.created_by); END IF;
  IF TG_OP <> 'INSERT' THEN
    old_agreement := OLD.agreement_id; old_year := OLD.report_year; old_period := OLD.period_type;
    INSERT INTO agreement_report_history(agreement_id,report_year,period_type,from_status,to_status,comment,changed_by)
    SELECT agreement_id,report_year,period_type,status,'draft','Автоматически: изменена запись плана/факта',
      actor
    FROM agreement_reports WHERE agreement_id=old_agreement AND report_year=old_year
      AND period_type=old_period AND status<>'draft';
    UPDATE agreement_reports SET status='draft', scope_confirmed=FALSE,
      conditions_confirmed=FALSE, evidence_confirmed=FALSE, counterparty_confirmed=FALSE,
      ready_by=NULL,ready_at=NULL,verified_by=NULL,verified_at=NULL,
      approved_by=NULL,approved_at=NULL,updated_at=now()
    WHERE agreement_id=old_agreement AND report_year=old_year AND period_type=old_period
      AND status<>'draft';
  END IF;
  IF TG_OP <> 'DELETE' THEN
    new_agreement := NEW.agreement_id; new_year := NEW.report_year; new_period := NEW.period_type;
    IF TG_OP='INSERT' OR old_agreement IS DISTINCT FROM new_agreement
       OR old_year IS DISTINCT FROM new_year OR old_period IS DISTINCT FROM new_period THEN
      INSERT INTO agreement_report_history(agreement_id,report_year,period_type,from_status,to_status,comment,changed_by)
      SELECT agreement_id,report_year,period_type,status,'draft','Автоматически: добавлена или перенесена запись плана/факта',
        actor
      FROM agreement_reports WHERE agreement_id=new_agreement AND report_year=new_year
        AND period_type=new_period AND status<>'draft';
      UPDATE agreement_reports SET status='draft', scope_confirmed=FALSE,
        conditions_confirmed=FALSE, evidence_confirmed=FALSE, counterparty_confirmed=FALSE,
        ready_by=NULL,ready_at=NULL,verified_by=NULL,verified_at=NULL,
        approved_by=NULL,approved_at=NULL,updated_at=now()
      WHERE agreement_id=new_agreement AND report_year=new_year AND period_type=new_period
        AND status<>'draft';
    END IF;
  END IF;
  IF TG_OP='DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER entries_reset_report_workflow
AFTER INSERT OR UPDATE OR DELETE ON entries
FOR EACH ROW EXECUTE FUNCTION reset_agreement_report_on_entry_change();

CREATE OR REPLACE FUNCTION reset_agreement_reports_on_scope_change()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO agreement_report_history(agreement_id,report_year,period_type,from_status,to_status,comment,changed_by)
  SELECT agreement_id,report_year,period_type,status,'draft','Автоматически: изменены реквизиты соглашения',COALESCE(NEW.updated_by,NEW.created_by)
  FROM agreement_reports WHERE agreement_id=NEW.id AND status<>'draft';
  UPDATE agreement_reports SET status='draft', scope_confirmed=FALSE,
    conditions_confirmed=FALSE, evidence_confirmed=FALSE, counterparty_confirmed=FALSE,
    ready_by=NULL,ready_at=NULL,verified_by=NULL,verified_at=NULL,
    approved_by=NULL,approved_at=NULL,updated_at=now()
  WHERE agreement_id=CASE WHEN TG_OP='DELETE' THEN OLD.agreement_id ELSE NEW.agreement_id END AND status<>'draft';
  IF TG_OP='DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER agreement_scope_resets_report_workflow
AFTER INSERT OR UPDATE OR DELETE ON agreement_activity_requirements
FOR EACH ROW EXECUTE FUNCTION reset_agreement_reports_on_scope_change();

-- Agreement edits can change dates, signature, status or participating OOs.
CREATE OR REPLACE FUNCTION reset_reports_on_agreement_change()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE agreement_reports SET status='draft', scope_confirmed=FALSE,
    conditions_confirmed=FALSE, evidence_confirmed=FALSE, counterparty_confirmed=FALSE,
    ready_by=NULL,ready_at=NULL,verified_by=NULL,verified_at=NULL,
    approved_by=NULL,approved_at=NULL,updated_at=now()
  WHERE agreement_id=NEW.id AND status<>'draft';
  RETURN NEW;
END $$;
CREATE TRIGGER agreements_reset_report_workflow
AFTER UPDATE ON agreements
FOR EACH ROW EXECUTE FUNCTION reset_reports_on_agreement_change();

CREATE OR REPLACE FUNCTION reset_reports_on_authority_change()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status IS DISTINCT FROM NEW.status THEN
    INSERT INTO agreement_report_history(agreement_id,report_year,period_type,from_status,to_status,comment,changed_by)
    SELECT report.agreement_id,report.report_year,report.period_type,report.status,'draft','Автоматически: изменён статус РОИВ',COALESCE(NEW.updated_by,NEW.created_by)
    FROM agreement_reports report JOIN agreements agreement ON agreement.id=report.agreement_id
    WHERE agreement.regional_authority_id=NEW.id AND report.status<>'draft';
    UPDATE agreement_reports report SET status='draft',scope_confirmed=FALSE,
      conditions_confirmed=FALSE,evidence_confirmed=FALSE,counterparty_confirmed=FALSE,
      ready_by=NULL,ready_at=NULL,verified_by=NULL,verified_at=NULL,
      approved_by=NULL,approved_at=NULL,updated_at=now()
    FROM agreements agreement
    WHERE report.agreement_id=agreement.id AND agreement.regional_authority_id=NEW.id
      AND report.status<>'draft';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER regional_authority_resets_report_workflow
AFTER UPDATE OF status ON regional_authorities
FOR EACH ROW EXECUTE FUNCTION reset_reports_on_authority_change();

DROP VIEW entry_eligibility;
CREATE VIEW entry_eligibility AS
SELECT e.id,e.partner_id,e.agreement_id,e.report_year,e.period_type,e.amount_rub,
  COALESCE(r.status,'draft') AS report_status,
  COALESCE(
    r.status='approved'
    AND r.scope_confirmed AND r.conditions_confirmed AND r.evidence_confirmed
    AND (e.period_type='plan' OR r.counterparty_confirmed)
    AND a.status='active'
    AND a.valid_from<=make_date(e.report_year,12,31)
    AND a.valid_until>=make_date(e.report_year,1,1)
    AND (a.agreement_kind<>'roiv' OR EXISTS(
      SELECT 1 FROM regional_authorities ra
      WHERE ra.id=a.regional_authority_id AND ra.status='active'))
    AND EXISTS(SELECT 1 FROM agreement_responsible_people rp
      WHERE rp.agreement_id=a.id AND rp.party='cyberprotect')
    AND EXISTS(SELECT 1 FROM agreement_responsible_people rp
      WHERE rp.agreement_id=a.id AND rp.party='counterparty')
    AND (
      NOT EXISTS(
        SELECT 1 FROM agreement_activity_requirements req
        WHERE req.agreement_id=e.agreement_id AND NOT EXISTS(
          SELECT 1 FROM entries covered
          WHERE covered.agreement_id=e.agreement_id
            AND covered.report_year=e.report_year
            AND covered.period_type=e.period_type
            AND covered.category_code=req.category_code
            AND covered.amount_rub>0))
      OR (
        a.agreement_kind='education_organization'
        AND EXISTS(SELECT 1 FROM entries top_entry
          WHERE top_entry.agreement_id=e.agreement_id
            AND top_entry.report_year=e.report_year
            AND top_entry.period_type=e.period_type
            AND top_entry.category_code='top_it' AND top_entry.amount_rub>0)
        AND EXISTS(
          SELECT 1 FROM agreements other
          JOIN agreement_reports other_report ON other_report.agreement_id=other.id
            AND other_report.report_year=e.report_year
            AND other_report.period_type=e.period_type
            AND other_report.status='approved'
          JOIN agreement_partners other_partner ON other_partner.agreement_id=other.id
          JOIN partners other_organization ON other_organization.id=other_partner.partner_id
            AND other_organization.partner_kind<>'school'
          WHERE other.id<>e.agreement_id AND other.status='active'
            AND EXISTS(SELECT 1 FROM agreement_partners current_partner
              WHERE current_partner.agreement_id=e.agreement_id
                AND current_partner.partner_id<>other_partner.partner_id)
            AND EXISTS(SELECT 1 FROM entries x WHERE x.agreement_id=other.id AND x.report_year=e.report_year AND x.period_type=e.period_type AND x.category_code='teachers' AND x.amount_rub>0)
            AND EXISTS(SELECT 1 FROM entries x WHERE x.agreement_id=other.id AND x.report_year=e.report_year AND x.period_type=e.period_type AND x.category_code='ood_rpd' AND x.amount_rub>0)
            AND EXISTS(SELECT 1 FROM entries x WHERE x.agreement_id=other.id AND x.report_year=e.report_year AND x.period_type=e.period_type AND x.category_code IN ('internship','employment_practice') AND x.amount_rub>0)
            AND EXISTS(SELECT 1 FROM entries x WHERE x.agreement_id=other.id AND x.report_year=e.report_year AND x.period_type=e.period_type AND x.category_code='minc_decision' AND x.amount_rub>0)
        )
      )
    ),FALSE) AS eligible
FROM entries e
JOIN agreements a ON a.id=e.agreement_id
LEFT JOIN agreement_reports r ON r.agreement_id=e.agreement_id
  AND r.report_year=e.report_year AND r.period_type=e.period_type;
