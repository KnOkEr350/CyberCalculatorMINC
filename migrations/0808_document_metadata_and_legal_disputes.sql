-- DATA-12 / INT-08: типизированный документ описывается целиком, а юридическое
-- сомнение — самостоятельное состояние записи с причиной.
--
-- Тип, статус проверки, время загрузки, срок хранения и ссылка на blob у
-- документа уже были (0801, 0803). Здесь добавляются владелец, реквизиты и
-- сведения о подписи и сертификате.
ALTER TABLE attachments
  ADD COLUMN owner_role TEXT,
  ADD COLUMN owner_entity_type TEXT,
  ADD COLUMN document_number TEXT,
  ADD COLUMN document_date DATE,
  ADD COLUMN signer_name TEXT,
  ADD COLUMN certificate_serial TEXT,
  ADD COLUMN certificate_valid_from DATE,
  ADD COLUMN certificate_valid_until DATE,
  ADD COLUMN signature_verified_at TIMESTAMPTZ,
  ADD COLUMN signature_verified_by UUID REFERENCES users(id);

-- Владелец — сторона, загрузившая документ: роль и тип профиля загрузившего на
-- момент загрузки. Существующие документы получают владельца по загрузившему.
UPDATE attachments a SET owner_role = u.role, owner_entity_type = COALESCE(u.entity_type, 'organization')
FROM users u WHERE u.id = a.uploaded_by;

ALTER TABLE attachments
  ALTER COLUMN owner_role SET NOT NULL,
  ALTER COLUMN owner_entity_type SET NOT NULL,
  ADD CONSTRAINT attachments_owner_entity_check CHECK (owner_entity_type IN ('organization','edu_institution')),
  ADD CONSTRAINT attachments_document_number_length CHECK (document_number IS NULL OR length(btrim(document_number)) BETWEEN 1 AND 200),
  ADD CONSTRAINT attachments_signer_name_length CHECK (signer_name IS NULL OR length(btrim(signer_name)) BETWEEN 2 AND 300),
  ADD CONSTRAINT attachments_certificate_serial_format CHECK (certificate_serial IS NULL OR certificate_serial ~ '^[0-9A-F]{6,64}$'),
  -- Сведения о подписи задаются целиком: подписант без сертификата и срок без
  -- подписанта ничего не подтверждают.
  ADD CONSTRAINT attachments_signature_metadata_complete CHECK (
    (signer_name IS NULL AND certificate_serial IS NULL AND certificate_valid_from IS NULL AND certificate_valid_until IS NULL)
    OR (signer_name IS NOT NULL AND certificate_serial IS NOT NULL AND certificate_valid_from IS NOT NULL AND certificate_valid_until IS NOT NULL
        AND certificate_valid_until >= certificate_valid_from)),
  -- Проверить можно только то, что описано: подпись без сведений о ней не
  -- верифицируется.
  ADD CONSTRAINT attachments_signature_verification_consistency CHECK (
    (signature_verified_at IS NULL) = (signature_verified_by IS NULL)
    AND (signature_verified_at IS NULL OR signer_name IS NOT NULL));

-- Владельца не задаёт клиент: он берётся у загрузившего при вставке и после
-- этого не меняется.
CREATE OR REPLACE FUNCTION attachments_owner_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE uploader_role TEXT; uploader_entity TEXT;
BEGIN
  IF TG_OP = 'INSERT' THEN
    SELECT role, COALESCE(entity_type, 'organization') INTO uploader_role, uploader_entity
      FROM public.users WHERE id = NEW.uploaded_by;
    NEW.owner_role := uploader_role;
    NEW.owner_entity_type := uploader_entity;
    RETURN NEW;
  END IF;
  IF NEW.owner_role IS DISTINCT FROM OLD.owner_role OR NEW.owner_entity_type IS DISTINCT FROM OLD.owner_entity_type THEN
    RAISE EXCEPTION 'attachment owner is immutable';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER attachments_owner
BEFORE INSERT OR UPDATE ON attachments
FOR EACH ROW EXECUTE FUNCTION attachments_owner_guard();

-- Юридическое сомнение (ADR-17): юрист не меняет данные, но может поставить
-- запись под сомнение с причиной; до снятия она не входит в зачётную сумму.
CREATE TABLE legal_disputes (
  id BIGSERIAL PRIMARY KEY,
  entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
  attachment_id UUID REFERENCES attachments(id) ON DELETE SET NULL,
  reason TEXT NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 2000),
  raised_by UUID NOT NULL REFERENCES users(id),
  raised_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  lifted_by UUID REFERENCES users(id),
  lifted_at TIMESTAMPTZ,
  lifted_reason TEXT CHECK (lifted_reason IS NULL OR length(btrim(lifted_reason)) BETWEEN 1 AND 2000),
  CHECK ((lifted_at IS NULL) = (lifted_by IS NULL) AND (lifted_at IS NULL) = (lifted_reason IS NULL))
);

-- У записи не больше одного действующего сомнения.
CREATE UNIQUE INDEX legal_disputes_one_active_idx ON legal_disputes(entry_id) WHERE lifted_at IS NULL;
CREATE INDEX legal_disputes_entry_idx ON legal_disputes(entry_id, id);

-- История сомнений не переписывается: единственное допустимое изменение — снять
-- действующее сомнение, заполнив три поля снятия. Удаление возможно только
-- каскадом при удалении самой записи.
CREATE OR REPLACE FUNCTION legal_disputes_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF pg_trigger_depth() > 1 THEN RETURN OLD; END IF;
    RAISE EXCEPTION 'legal disputes are not deleted';
  END IF;
  IF OLD.lifted_at IS NULL AND NEW.lifted_at IS NOT NULL
     AND NEW.entry_id = OLD.entry_id AND NEW.reason = OLD.reason AND NEW.raised_by = OLD.raised_by
     AND NEW.raised_at = OLD.raised_at AND NEW.attachment_id IS NOT DISTINCT FROM OLD.attachment_id THEN
    RETURN NEW;
  END IF;
  -- Снятие документа с записи (attachment_id → NULL при удалении вложения)
  -- меняет только ссылку.
  IF NEW.attachment_id IS NULL AND OLD.attachment_id IS NOT NULL
     AND NEW.entry_id = OLD.entry_id AND NEW.reason = OLD.reason AND NEW.raised_by = OLD.raised_by
     AND NEW.raised_at = OLD.raised_at AND NEW.lifted_at IS NOT DISTINCT FROM OLD.lifted_at
     AND NEW.lifted_by IS NOT DISTINCT FROM OLD.lifted_by AND NEW.lifted_reason IS NOT DISTINCT FROM OLD.lifted_reason THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'legal disputes are immutable';
END $$;

CREATE TRIGGER legal_disputes_immutable
BEFORE UPDATE OR DELETE ON legal_disputes
FOR EACH ROW EXECUTE FUNCTION legal_disputes_guard();

-- Запись под действующим юридическим сомнением не допускается к зачёту: граница
-- зачёта (entry_eligibility) одна для дашбордов, отчётов и выгрузок.
CREATE OR REPLACE VIEW entry_eligibility AS
SELECT e.id,e.partner_id,e.agreement_id,e.report_year,e.period_type,e.amount_rub,
  COALESCE(r.status,'draft') AS report_status,
  COALESCE(
    r.status='approved'
    AND NOT EXISTS(SELECT 1 FROM legal_disputes d WHERE d.entry_id=e.id AND d.lifted_at IS NULL)
    AND r.scope_confirmed AND r.conditions_confirmed AND r.evidence_confirmed
    AND (e.period_type='plan' OR r.counterparty_confirmed)
    AND (e.cost_method<>'actual' OR (
      r.actual_costs_confirmed AND NULLIF(trim(r.auditor_report_reference),'') IS NOT NULL))
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
            AND other.it_company_id=a.it_company_id
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
