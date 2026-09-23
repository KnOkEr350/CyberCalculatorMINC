-- MIN-03: immutable revision history for confirmed Type 5 costs.
CREATE TABLE ministry_cost_revisions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  entry_id UUID NOT NULL REFERENCES entries(id) ON DELETE RESTRICT,
  revision_no INTEGER NOT NULL CHECK(revision_no > 0),
  previous_amount_rub NUMERIC(16,2),
  confirmed_amount_rub NUMERIC(16,2) NOT NULL CHECK(confirmed_amount_rub > 0),
  calculation_basis TEXT NOT NULL CHECK(length(btrim(calculation_basis)) BETWEEN 1 AND 2000),
  correction_reason TEXT NOT NULL CHECK(length(btrim(correction_reason)) BETWEEN 1 AND 2000),
  changed_by UUID NOT NULL REFERENCES users(id),
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(entry_id,revision_no)
);

INSERT INTO ministry_cost_revisions(entry_id,revision_no,confirmed_amount_rub,calculation_basis,correction_reason,changed_by,changed_at)
SELECT id,1,amount_rub,COALESCE(NULLIF(btrim(payload->>'calculation_basis'),''),'Перенесено из ранее созданной записи'),
  'Начальная редакция, перенесённая при типизации истории стоимости',created_by,created_at
FROM entries
WHERE category_code='minc_decision' AND amount_rub>0;

CREATE INDEX ministry_cost_revisions_entry_idx
  ON ministry_cost_revisions(entry_id,revision_no DESC);

CREATE FUNCTION ministry_cost_revisions_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'ministry cost revision history is append-only';
END $$;

CREATE TRIGGER ministry_cost_revisions_no_update
BEFORE UPDATE OR DELETE ON ministry_cost_revisions
FOR EACH ROW EXECUTE FUNCTION ministry_cost_revisions_append_only();
