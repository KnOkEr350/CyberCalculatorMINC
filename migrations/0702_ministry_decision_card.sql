-- MIN-01 / MIN-05: typed Ministry decision card and deterministic legacy
-- backfill. Missing historical facts are never invented; such rows remain
-- available with an explicit manual_review status until an operator saves the
-- completed card through the normal API.
ALTER TABLE entries
  ADD COLUMN ministry_instruction_type TEXT,
  ADD COLUMN ministry_instruction_authority TEXT,
  ADD COLUMN ministry_instruction_reference TEXT,
  ADD COLUMN ministry_decision_number TEXT,
  ADD COLUMN ministry_decision_date DATE,
  ADD COLUMN ministry_implementation_start DATE,
  ADD COLUMN ministry_implementation_deadline DATE,
  ADD COLUMN ministry_implementation_conditions TEXT,
  ADD COLUMN ministry_activity_description TEXT,
  ADD COLUMN ministry_card_backfill_status TEXT;

UPDATE entries SET
  ministry_instruction_type = COALESCE(NULLIF(btrim(payload->>'instruction_type'),''), CASE payload->>'instruction_authority'
    WHEN 'president' THEN 'president_instruction'
    WHEN 'prime_minister' THEN 'government_instruction'
    WHEN 'deputy_prime_minister' THEN 'curator_instruction'
    WHEN 'security_council' THEN 'security_council_decision'
  END),
  ministry_instruction_authority = NULLIF(btrim(payload->>'instruction_authority'),''),
  ministry_instruction_reference = NULLIF(btrim(payload->>'instruction_reference'),''),
  ministry_decision_number = COALESCE(NULLIF(btrim(payload->>'decision_number'),''),NULLIF(btrim(payload->>'decision_reference'),'')),
  ministry_decision_date = CASE WHEN pg_input_is_valid(payload->>'decision_date','date') THEN (payload->>'decision_date')::date END,
  ministry_implementation_start = CASE WHEN pg_input_is_valid(payload->>'implementation_start','date') THEN (payload->>'implementation_start')::date END,
  ministry_implementation_deadline = CASE WHEN pg_input_is_valid(payload->>'implementation_deadline','date') THEN (payload->>'implementation_deadline')::date END,
  ministry_implementation_conditions = NULLIF(btrim(payload->>'implementation_conditions'),''),
  ministry_activity_description = NULLIF(btrim(payload->>'activity_description'),''),
  ministry_card_backfill_status = 'manual_review'
WHERE category_code='minc_decision';

UPDATE entries SET ministry_card_backfill_status='complete'
WHERE category_code='minc_decision'
  AND ministry_instruction_type IS NOT NULL
  AND ministry_instruction_authority IS NOT NULL
  AND ministry_instruction_reference IS NOT NULL
  AND ministry_decision_number IS NOT NULL
  AND ministry_decision_date IS NOT NULL
  AND ministry_implementation_start IS NOT NULL
  AND ministry_implementation_deadline IS NOT NULL
  AND ministry_implementation_conditions IS NOT NULL
  AND ministry_activity_description IS NOT NULL
  AND (
    (ministry_instruction_type='president_instruction' AND ministry_instruction_authority='president')
    OR (ministry_instruction_type='government_instruction' AND ministry_instruction_authority='prime_minister')
    OR (ministry_instruction_type='curator_instruction' AND ministry_instruction_authority='deputy_prime_minister')
    OR (ministry_instruction_type='security_council_decision' AND ministry_instruction_authority='security_council')
  )
  AND ministry_implementation_deadline >= ministry_implementation_start
  AND ministry_implementation_deadline >= ministry_decision_date
  AND length(btrim(ministry_instruction_reference)) BETWEEN 1 AND 1000
  AND length(btrim(ministry_decision_number)) BETWEEN 1 AND 200
  AND length(btrim(ministry_implementation_conditions)) BETWEEN 1 AND 4000
  AND length(btrim(ministry_activity_description)) BETWEEN 1 AND 2000;

ALTER TABLE entries ADD CONSTRAINT entries_ministry_card_values CHECK (
  category_code <> 'minc_decision'
  OR (
    ministry_card_backfill_status IS NOT NULL
    AND (
      ministry_card_backfill_status='manual_review'
      OR (
        ministry_card_backfill_status='complete'
        AND ministry_instruction_type IS NOT NULL
        AND ministry_instruction_authority IS NOT NULL
        AND ministry_instruction_reference IS NOT NULL
        AND ministry_decision_number IS NOT NULL
        AND ministry_decision_date IS NOT NULL
        AND ministry_implementation_start IS NOT NULL
        AND ministry_implementation_deadline IS NOT NULL
        AND ministry_implementation_conditions IS NOT NULL
        AND ministry_activity_description IS NOT NULL
        AND ministry_instruction_type IN ('president_instruction','government_instruction','curator_instruction','security_council_decision')
        AND ministry_instruction_authority IN ('president','prime_minister','deputy_prime_minister','security_council')
        AND (
          (ministry_instruction_type='president_instruction' AND ministry_instruction_authority='president')
          OR (ministry_instruction_type='government_instruction' AND ministry_instruction_authority='prime_minister')
          OR (ministry_instruction_type='curator_instruction' AND ministry_instruction_authority='deputy_prime_minister')
          OR (ministry_instruction_type='security_council_decision' AND ministry_instruction_authority='security_council')
        )
        AND length(btrim(ministry_instruction_reference)) BETWEEN 1 AND 1000
        AND length(btrim(ministry_decision_number)) BETWEEN 1 AND 200
        AND ministry_implementation_deadline >= ministry_implementation_start
        AND ministry_implementation_deadline >= ministry_decision_date
        AND length(btrim(ministry_implementation_conditions)) BETWEEN 1 AND 4000
        AND length(btrim(ministry_activity_description)) BETWEEN 1 AND 2000
      )
    )
  )
);

CREATE INDEX entries_ministry_card_review_idx
  ON entries(ministry_card_backfill_status,report_year)
  WHERE category_code='minc_decision';
