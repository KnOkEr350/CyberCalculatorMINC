-- TCH-01: педагогическая нагрузка описывается атомарным ключом
-- «сотрудник + образовательная организация + дисциплина + год + период».
-- До этого одну и ту же нагрузку можно было внести дважды: расхождение
-- всплывало только при сверке итогов, когда исправлять его уже дорого.
--
-- Ключ строится по нормализованному названию дисциплины: «Разработка ПО» и
-- «  разработка по » — одна и та же дисциплина, а не две.
CREATE OR REPLACE FUNCTION teaching_discipline_key(payload JSONB) RETURNS TEXT
LANGUAGE sql IMMUTABLE AS $$
  SELECT lower(btrim(regexp_replace(COALESCE(payload->>'course_name',''), '\s+', ' ', 'g')))
$$;

-- Строки без сотрудника или без дисциплины в ключ не входят: они ещё не
-- описывают конкретную нагрузку и блокировать друг друга не должны.
CREATE UNIQUE INDEX teaching_load_atomic_key_idx ON entries (
  it_company_id,
  partner_id,
  report_year,
  period_type,
  (payload->>'staff_member_id'),
  teaching_discipline_key(payload),
  (payload->>'semester')
)
WHERE category_code = 'teachers'
  AND COALESCE(payload->>'staff_member_id','') <> ''
  AND teaching_discipline_key(payload) <> '';
