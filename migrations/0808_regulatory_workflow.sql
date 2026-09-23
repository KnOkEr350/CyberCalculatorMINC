-- WF-01: explicit state machine for agreement, preliminary-list and final-list
-- processes. It coexists with the legacy four-state report approval during
-- the migration described by BASE-11.
CREATE TABLE regulatory_workflows (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
  report_year INTEGER NOT NULL CHECK(report_year BETWEEN 2000 AND 2100),
  process_type TEXT NOT NULL CHECK(process_type IN ('agreement','preliminary','final')),
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN (
    'draft','sent','in_review','rework','resubmitted','approved','default_approved','disputed'
  )),
  version BIGINT NOT NULL DEFAULT 1,
  sent_at TIMESTAMPTZ,
  review_due_on DATE,
  decided_at TIMESTAMPTZ,
  updated_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(agreement_id,report_year,process_type)
);
CREATE INDEX regulatory_workflows_queue_idx
  ON regulatory_workflows(process_type,status,review_due_on,report_year);

CREATE TABLE regulatory_workflow_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_id UUID NOT NULL REFERENCES regulatory_workflows(id) ON DELETE CASCADE,
  from_status TEXT NOT NULL,
  to_status TEXT NOT NULL,
  reason TEXT NOT NULL,
  changed_by UUID NOT NULL REFERENCES users(id),
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(workflow_id,changed_at,id)
);
CREATE INDEX regulatory_workflow_history_idx
  ON regulatory_workflow_history(workflow_id,changed_at,id);

