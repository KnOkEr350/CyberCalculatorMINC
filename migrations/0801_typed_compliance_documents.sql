-- Typed evidence for the readiness/risk engine. Existing files remain
-- available and are explicitly classified as "other" until reviewed.
ALTER TABLE attachments
  ADD COLUMN document_type TEXT NOT NULL DEFAULT 'other',
  ADD COLUMN review_status TEXT NOT NULL DEFAULT 'pending',
  ADD COLUMN reviewed_by UUID REFERENCES users(id),
  ADD COLUMN reviewed_at TIMESTAMPTZ,
  ADD COLUMN review_comment TEXT;

ALTER TABLE attachments
  ADD CONSTRAINT attachments_document_type_check CHECK (document_type IN (
    'other','employment_contract','organization_agreement','individual_plan',
    'appointment_order','program_project','expert_conclusion',
    'academic_council_protocol','internship_agreement','practice_agreement',
    'labor_contract','mentor_order','individual_program',
    'incoming_certificate','outgoing_certificate','top_agreement',
    'payment_order','spending_act','ano_letter','school_agreement',
    'participant_groups','acceptance_act','digital_trace',
    'ministry_decision','expense_evidence','auditor_report'
  )),
  ADD CONSTRAINT attachments_review_status_check CHECK (review_status IN ('pending','approved','rejected')),
  ADD CONSTRAINT attachments_review_consistency_check CHECK (
    (review_status='pending' AND reviewed_by IS NULL AND reviewed_at IS NULL)
    OR (review_status IN ('approved','rejected') AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL)
  );

CREATE INDEX attachments_entry_document_type_idx
  ON attachments(entry_id, document_type, retention_expires_at);

-- Store both the N-2 savings basis and the already calculated 3% minimum.
-- Legacy rows keep a NULL basis and remain visibly unverified.
ALTER TABLE organization_budget_targets
  ADD COLUMN savings_base_rub NUMERIC(16,2),
  ADD COLUMN source_reference TEXT,
  ADD COLUMN notified_at DATE,
  ADD CONSTRAINT organization_budget_savings_positive CHECK (savings_base_rub IS NULL OR savings_base_rub > 0),
  ADD CONSTRAINT organization_budget_source_length CHECK (source_reference IS NULL OR length(source_reference) <= 2000);
