-- Preserve legacy targets as history; the most recently updated value becomes
-- the shared organization target. No existing entry amount is recalculated.
CREATE TABLE organization_budget_targets (
  report_year INTEGER PRIMARY KEY CHECK(report_year BETWEEN 2000 AND 2100),
  target_amount_rub NUMERIC(16,2) NOT NULL CHECK(target_amount_rub>0),
  updated_by UUID REFERENCES users(id),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO organization_budget_targets(report_year,target_amount_rub,updated_by,updated_at)
SELECT DISTINCT ON(report_year) report_year,target_amount_rub,updated_by,updated_at
FROM budget_targets WHERE report_year BETWEEN 2000 AND 2100 AND target_amount_rub>0
ORDER BY report_year,updated_at DESC,owner_user_id;
