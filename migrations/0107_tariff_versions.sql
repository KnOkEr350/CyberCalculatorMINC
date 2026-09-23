-- DATA-07: ставки Методики Приказа № 270 хранятся версиями с периодом действия
-- и ссылкой на официальную редакцию. До этой миграции ставки были записаны в
-- коде дважды — в точном и в приближённом расчёте — и менялись только правкой
-- исходников, без следа о том, по какой редакции посчитана сумма.
--
-- Редакция действует для отчётных годов, начинающихся не раньше её valid_from:
-- сумма мероприятия не должна менять смысл задним числом внутри года.
CREATE TABLE tariff_rates (
  code TEXT PRIMARY KEY CHECK (code ~ '^[a-z_]+(\.[a-z_]+)+$'),
  category_code TEXT NOT NULL REFERENCES activity_categories(code),
  label TEXT NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 200),
  unit TEXT NOT NULL CHECK (length(btrim(unit)) BETWEEN 1 AND 40)
);

CREATE TABLE tariff_versions (
  id BIGSERIAL PRIMARY KEY,
  rate_code TEXT NOT NULL REFERENCES tariff_rates(code),
  amount_rub NUMERIC(16,2) NOT NULL CHECK (amount_rub > 0),
  valid_from DATE NOT NULL,
  valid_until DATE,
  source_reference TEXT NOT NULL CHECK (length(btrim(source_reference)) BETWEEN 1 AND 2000),
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (valid_until IS NULL OR valid_until >= valid_from),
  -- Редакции меняются с начала года: выбор идёт по началу отчётного года.
  CHECK (extract(month FROM valid_from) = 1 AND extract(day FROM valid_from) = 1),
  UNIQUE (rate_code, valid_from)
);

CREATE INDEX tariff_versions_lookup_idx ON tariff_versions(rate_code, valid_from DESC);

-- Версии не переписываются и не пересекаются. Единственное допустимое
-- изменение — закрыть действующую версию датой окончания, когда вступает в силу
-- следующая; сумма, начало и основание остаются как были.
CREATE OR REPLACE FUNCTION guard_tariff_versions() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'tariff versions are immutable';
  END IF;
  IF TG_OP = 'UPDATE' THEN
    IF NEW.valid_until IS NOT NULL AND OLD.valid_until IS NULL
       AND NEW.rate_code = OLD.rate_code AND NEW.amount_rub = OLD.amount_rub
       AND NEW.valid_from = OLD.valid_from AND NEW.source_reference = OLD.source_reference
       AND NEW.created_at = OLD.created_at AND NEW.created_by IS NOT DISTINCT FROM OLD.created_by THEN
      RETURN NEW;
    END IF;
    RAISE EXCEPTION 'tariff versions are immutable';
  END IF;
  -- INSERT: период не пересекается с уже существующими версиями той же ставки.
  PERFORM pg_advisory_xact_lock(hashtext('tariff:' || NEW.rate_code));
  IF EXISTS (
    SELECT 1 FROM public.tariff_versions existing
    WHERE existing.rate_code = NEW.rate_code
      AND daterange(existing.valid_from, existing.valid_until, '[]')
          && daterange(NEW.valid_from, NEW.valid_until, '[]')
  ) THEN
    RAISE EXCEPTION 'tariff version overlaps an existing period';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER tariff_versions_guard
BEFORE INSERT OR UPDATE OR DELETE ON tariff_versions
FOR EACH ROW EXECUTE FUNCTION guard_tariff_versions();

-- Основание суммы мероприятия: версии ставок, по которым она посчитана.
ALTER TABLE entries ADD COLUMN formula_tariff_ids BIGINT[];

INSERT INTO tariff_rates(code,category_code,label,unit) VALUES
  ('teachers.academic_hour.vuz','teachers','Академический час преподавателя, ВО','ак. час'),
  ('teachers.academic_hour.kolledj','teachers','Академический час преподавателя, СПО','ак. час'),
  ('ood_rpd.rpd.vo.development','ood_rpd','РПД, ВО: разработка','документ'),
  ('ood_rpd.rpd.vo.update','ood_rpd','РПД, ВО: актуализация','документ'),
  ('ood_rpd.rpd.vo.expertise','ood_rpd','РПД, ВО: экспертиза','документ'),
  ('ood_rpd.rpd.spo.development','ood_rpd','РПД, СПО: разработка','документ'),
  ('ood_rpd.rpd.spo.update','ood_rpd','РПД, СПО: актуализация','документ'),
  ('ood_rpd.rpd.spo.expertise','ood_rpd','РПД, СПО: экспертиза','документ'),
  ('ood_rpd.oop.vo.development','ood_rpd','ООП, ВО: разработка','документ'),
  ('ood_rpd.oop.vo.update','ood_rpd','ООП, ВО: актуализация','документ'),
  ('ood_rpd.oop.vo.expertise','ood_rpd','ООП, ВО: экспертиза','документ'),
  ('ood_rpd.oop.spo.development','ood_rpd','ООП, СПО: разработка','документ'),
  ('ood_rpd.oop.spo.update','ood_rpd','ООП, СПО: актуализация','документ'),
  ('ood_rpd.oop.spo.expertise','ood_rpd','ООП, СПО: экспертиза','документ'),
  ('internship.student_hour','internship','Час нагрузки студента','час'),
  ('internship.mentor_hour','internship','Час нагрузки наставника','час'),
  ('it_clubs.academic_hour','it_clubs','Академический час школьной программы','ак. час'),
  ('it_clubs.program_development','it_clubs','Разработка школьной программы','программа'),
  ('teacher_training.academic_hour','teacher_training','Академический час обучения учителя','ак. час'),
  ('teacher_training.program_development','teacher_training','Разработка программы обучения учителей','программа'),
  ('edu_content.student_platform_month','edu_content','Месяц доступа студента к платформе','месяц'),
  ('edu_content.teacher_platform_month','edu_content','Месяц доступа учителя к платформе','месяц');

-- Редакция поставки действует с 2000 года без даты окончания: суммы
-- мероприятий за прошлые годы пересчитываться не должны.
INSERT INTO tariff_versions(rate_code,amount_rub,valid_from,source_reference) VALUES
  ('teachers.academic_hour.vuz',4140,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('teachers.academic_hour.kolledj',3900,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.rpd.vo.development',300000,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.rpd.vo.update',160000,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.rpd.vo.expertise',55000,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.rpd.spo.development',270750,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.rpd.spo.update',150000,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.rpd.spo.expertise',58060,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.oop.vo.development',2039850,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.oop.vo.update',626110,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.oop.vo.expertise',312300,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.oop.spo.development',1731360,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.oop.spo.update',427440,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('ood_rpd.oop.spo.expertise',171000,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('internship.student_hour',800,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('internship.mentor_hour',2390,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('it_clubs.academic_hour',4260,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('it_clubs.program_development',530890,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('teacher_training.academic_hour',3790,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('teacher_training.program_development',1408570,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('edu_content.student_platform_month',6800,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)'),
  ('edu_content.teacher_platform_month',8590,DATE '2000-01-01','Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)');
