-- INT-03: typed mentor assignment period and appointment order.
CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE entries
  ADD COLUMN mentor_id UUID REFERENCES mentors(id),
  ADD COLUMN mentor_assignment_start DATE,
  ADD COLUMN mentor_assignment_end DATE,
  ADD COLUMN mentor_order_number TEXT,
  ADD COLUMN mentor_order_date DATE,
  ADD COLUMN assigned_student_name TEXT;

UPDATE entries SET
  mentor_id = CASE WHEN EXISTS(SELECT 1 FROM mentors m WHERE m.id::text=payload->>'mentor_id') THEN (payload->>'mentor_id')::uuid END,
  mentor_assignment_start = CASE WHEN payload->>'mentor_assignment_start' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN (payload->>'mentor_assignment_start')::date END,
  mentor_assignment_end = CASE WHEN payload->>'mentor_assignment_end' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN (payload->>'mentor_assignment_end')::date END,
  mentor_order_number = NULLIF(btrim(payload->>'mentor_order_number'),''),
  mentor_order_date = CASE WHEN payload->>'mentor_order_date' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN (payload->>'mentor_order_date')::date END,
  assigned_student_name = NULLIF(lower(btrim(payload->>'student_full_name')),'')
WHERE category_code IN ('internship','employment_practice');

-- Historical payloads did not require this complete tuple. Keep them visible
-- for manual completion without turning a missing date into an unbounded
-- exclusion range that would block the migration.
UPDATE entries SET
  mentor_id=NULL,
  mentor_assignment_start=NULL,
  mentor_assignment_end=NULL,
  mentor_order_number=NULL,
  mentor_order_date=NULL,
  assigned_student_name=NULL
WHERE category_code IN ('internship','employment_practice')
  AND (mentor_id IS NULL OR mentor_assignment_start IS NULL OR mentor_assignment_end IS NULL
    OR mentor_order_number IS NULL OR mentor_order_date IS NULL OR assigned_student_name IS NULL);

ALTER TABLE entries ADD CONSTRAINT entries_mentor_assignment_consistent CHECK (
  category_code NOT IN ('internship','employment_practice')
  OR (mentor_id IS NULL AND mentor_assignment_start IS NULL AND mentor_assignment_end IS NULL
    AND mentor_order_number IS NULL AND mentor_order_date IS NULL AND assigned_student_name IS NULL)
  OR (
    mentor_assignment_start IS NOT NULL
    AND mentor_assignment_end IS NOT NULL
    AND mentor_assignment_end >= mentor_assignment_start
    AND length(btrim(COALESCE(mentor_order_number,''))) BETWEEN 1 AND 100
    AND mentor_order_date IS NOT NULL
    AND length(btrim(COALESCE(assigned_student_name,''))) BETWEEN 2 AND 200
  )
);

-- One mentor may supervise several students at once, but the same student
-- cannot receive overlapping assignments from the same mentor.
ALTER TABLE entries ADD CONSTRAINT entries_mentor_student_period_no_overlap
  EXCLUDE USING gist (
    mentor_id WITH =,
    assigned_student_name WITH =,
    daterange(mentor_assignment_start, mentor_assignment_end, '[]') WITH &&
  ) WHERE (mentor_id IS NOT NULL AND mentor_assignment_start IS NOT NULL AND mentor_assignment_end IS NOT NULL
    AND assigned_student_name IS NOT NULL AND category_code IN ('internship','employment_practice'));

CREATE INDEX entries_mentor_assignment_idx
  ON entries(mentor_id,mentor_assignment_start,mentor_assignment_end)
  WHERE mentor_id IS NOT NULL;
