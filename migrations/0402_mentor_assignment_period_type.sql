-- План и факт — разные срезы одной карточки: назначение наставника студенту
-- на те же даты обязано существовать в обоих. Прежнее ограничение (0400)
-- не учитывало period_type, поэтому факт по уже запланированной стажировке
-- или практике отклонялся как «пересекающееся назначение». Пересечение по-
-- прежнему запрещено внутри одного среза.
ALTER TABLE entries DROP CONSTRAINT entries_mentor_student_period_no_overlap;
ALTER TABLE entries ADD CONSTRAINT entries_mentor_student_period_no_overlap
  EXCLUDE USING gist (
    mentor_id WITH =,
    assigned_student_name WITH =,
    period_type WITH =,
    daterange(mentor_assignment_start, mentor_assignment_end, '[]') WITH &&
  ) WHERE (mentor_id IS NOT NULL AND mentor_assignment_start IS NOT NULL AND mentor_assignment_end IS NOT NULL
    AND assigned_student_name IS NOT NULL AND category_code IN ('internship','employment_practice'));
