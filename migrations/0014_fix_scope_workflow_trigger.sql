-- The scope table intentionally stores only the agreement/category pair.
-- Resolve the audit actor through the parent agreement instead of referring
-- to non-existent created_by/updated_by columns on the trigger row.
CREATE OR REPLACE FUNCTION reset_agreement_reports_on_scope_change()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  target_agreement UUID;
  actor UUID;
BEGIN
  IF TG_OP = 'DELETE' THEN
    target_agreement := OLD.agreement_id;
  ELSE
    target_agreement := NEW.agreement_id;
  END IF;

  SELECT COALESCE(updated_by, created_by)
  INTO actor
  FROM agreements
  WHERE id = target_agreement;

  INSERT INTO agreement_report_history(
    agreement_id,report_year,period_type,from_status,to_status,comment,changed_by
  )
  SELECT report.agreement_id,report.report_year,report.period_type,
    report.status,'draft','Автоматически: изменён перечень мероприятий соглашения',
    COALESCE(actor,report.updated_by,report.approved_by,report.verified_by,report.ready_by)
  FROM agreement_reports report
  WHERE report.agreement_id=target_agreement
    AND report.status<>'draft'
    AND COALESCE(actor,report.updated_by,report.approved_by,report.verified_by,report.ready_by) IS NOT NULL;

  UPDATE agreement_reports
  SET status='draft',scope_confirmed=FALSE,
    conditions_confirmed=FALSE,evidence_confirmed=FALSE,counterparty_confirmed=FALSE,
    ready_by=NULL,ready_at=NULL,verified_by=NULL,verified_at=NULL,
    approved_by=NULL,approved_at=NULL,updated_at=now(),updated_by=actor
  WHERE agreement_id=target_agreement AND status<>'draft';

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END $$;
