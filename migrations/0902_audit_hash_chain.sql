-- AUDIT-02: неизменяемость журнала подкрепляется цепочкой хешей. Триггер из
-- 0901 запрещает UPDATE и DELETE, но не замечает пропажу записи целиком —
-- например, при восстановлении таблицы из подменённой резервной копии.
-- Каждая запись теперь закрывает предыдущую: пропуск или подмена ломают цепь
-- и обнаруживаются проверкой.
ALTER TABLE audit_log
  ADD COLUMN chain_seq BIGSERIAL,
  ADD COLUMN prev_hash CHAR(64),
  ADD COLUMN row_hash CHAR(64);

-- AUDIT-03: очистка по сроку хранения переносит записи в архив вместе с их
-- звеньями цепи. Архив не разрушает неизменяемость: цепочка проверяется
-- сквозь него, а активная часть журнала не теряет связь с прошлым.
CREATE TABLE audit_log_archive (
  id UUID PRIMARY KEY,
  chain_seq BIGINT NOT NULL UNIQUE,
  entity_type TEXT NOT NULL,
  entity_id UUID,
  action TEXT NOT NULL,
  actor_type TEXT NOT NULL,
  user_id UUID,
  comment_text TEXT,
  old_value JSONB,
  new_value JSONB,
  request_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  prev_hash CHAR(64),
  row_hash CHAR(64) NOT NULL,
  archived_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION prevent_audit_archive_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit archive is append-only';
END $$;

CREATE TRIGGER audit_log_archive_append_only
BEFORE UPDATE OR DELETE ON audit_log_archive
FOR EACH ROW EXECUTE FUNCTION prevent_audit_archive_mutation();

-- Канонический вид записи для хеширования. Время берётся числом, а не текстом:
-- текстовое представление зависит от часового пояса сессии и сделало бы
-- проверку невоспроизводимой.
CREATE OR REPLACE FUNCTION audit_chain_payload(
  seq BIGINT, prev CHAR(64), actor TEXT, actor_id UUID, act TEXT,
  ent_type TEXT, ent_id UUID, old_val JSONB, new_val JSONB,
  req TEXT, note TEXT, at TIMESTAMPTZ
) RETURNS TEXT LANGUAGE sql IMMUTABLE AS $$
  SELECT concat_ws('|',
    COALESCE(prev,''), seq::text, actor, COALESCE(actor_id::text,''), act,
    ent_type, COALESCE(ent_id::text,''),
    COALESCE(old_val::text,''), COALESCE(new_val::text,''),
    req, COALESCE(note,''), extract(epoch from at)::numeric::text)
$$;

CREATE OR REPLACE FUNCTION audit_chain_link() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE previous CHAR(64);
BEGIN
  -- Вставки в журнал выстраиваются в один ряд: без этого две параллельные
  -- записи прочитали бы один и тот же конец цепи и раздвоили её.
  PERFORM pg_advisory_xact_lock(270901);
  SELECT row_hash INTO previous FROM public.audit_log ORDER BY chain_seq DESC LIMIT 1;
  IF previous IS NULL THEN
    SELECT row_hash INTO previous FROM public.audit_log_archive ORDER BY chain_seq DESC LIMIT 1;
  END IF;
  NEW.prev_hash := previous;
  NEW.row_hash := encode(sha256(convert_to(public.audit_chain_payload(
    NEW.chain_seq, previous, NEW.actor_type, NEW.user_id, NEW.action,
    NEW.entity_type, NEW.entity_id, NEW.old_value, NEW.new_value,
    NEW.request_id, NEW.comment_text, NEW.created_at), 'UTF8')), 'hex');
  RETURN NEW;
END $$;

-- Исторические записи получают звенья в порядке их появления. Запрет из 0901
-- на время расстановки снимается: он защищает журнал от правок приложением, а
-- здесь схема сама достраивает недостающие поля существующих строк.
DROP TRIGGER audit_log_append_only ON audit_log;

DO $$
DECLARE item RECORD; previous CHAR(64);
BEGIN
  FOR item IN SELECT * FROM audit_log ORDER BY chain_seq LOOP
    UPDATE audit_log SET prev_hash = previous,
      row_hash = encode(sha256(convert_to(audit_chain_payload(
        item.chain_seq, previous, item.actor_type, item.user_id, item.action,
        item.entity_type, item.entity_id, item.old_value, item.new_value,
        item.request_id, item.comment_text, item.created_at), 'UTF8')), 'hex')
      WHERE id = item.id
      RETURNING row_hash INTO previous;
  END LOOP;
END $$;

CREATE TRIGGER audit_log_append_only
BEFORE UPDATE OR DELETE ON audit_log
FOR EACH ROW EXECUTE FUNCTION prevent_audit_log_mutation();

ALTER TABLE audit_log ALTER COLUMN row_hash SET NOT NULL;
CREATE UNIQUE INDEX audit_log_chain_seq_idx ON audit_log(chain_seq);

CREATE TRIGGER audit_log_chain
BEFORE INSERT ON audit_log
FOR EACH ROW EXECUTE FUNCTION audit_chain_link();

-- verify_audit_chain проходит архив и активный журнал одной последовательностью
-- и возвращает первое нарушение: изменённое содержимое, разорванную связь или
-- пропущенное звено. Пустой результат означает целую цепь.
CREATE OR REPLACE FUNCTION verify_audit_chain()
RETURNS TABLE(chain_seq BIGINT, problem TEXT)
LANGUAGE plpgsql STABLE AS $$
DECLARE item RECORD; previous CHAR(64); expected_seq BIGINT; recomputed CHAR(64);
BEGIN
  expected_seq := NULL;
  FOR item IN
    SELECT a.chain_seq, a.actor_type, a.user_id, a.action, a.entity_type, a.entity_id,
           a.old_value, a.new_value, a.request_id, a.comment_text, a.created_at,
           a.prev_hash, a.row_hash
      FROM public.audit_log_archive a
    UNION ALL
    SELECT l.chain_seq, l.actor_type, l.user_id, l.action, l.entity_type, l.entity_id,
           l.old_value, l.new_value, l.request_id, l.comment_text, l.created_at,
           l.prev_hash, l.row_hash
      FROM public.audit_log l
    ORDER BY 1
  LOOP
    IF expected_seq IS NOT NULL AND item.chain_seq <> expected_seq THEN
      chain_seq := item.chain_seq; problem := 'пропущено звено'; RETURN NEXT; RETURN;
    END IF;
    IF item.prev_hash IS DISTINCT FROM previous THEN
      chain_seq := item.chain_seq; problem := 'разорвана связь с предыдущей записью'; RETURN NEXT; RETURN;
    END IF;
    recomputed := encode(sha256(convert_to(public.audit_chain_payload(
      item.chain_seq, previous, item.actor_type, item.user_id, item.action,
      item.entity_type, item.entity_id, item.old_value, item.new_value,
      item.request_id, item.comment_text, item.created_at), 'UTF8')), 'hex');
    IF recomputed <> item.row_hash THEN
      chain_seq := item.chain_seq; problem := 'содержимое записи изменено'; RETURN NEXT; RETURN;
    END IF;
    previous := item.row_hash;
    expected_seq := item.chain_seq + 1;
  END LOOP;
  RETURN;
END $$;

-- Очистка сначала архивирует запись со всеми звеньями и только затем удаляет
-- её из активного журнала: цепочка остаётся сплошной.
CREATE OR REPLACE FUNCTION purge_expired_audit() RETURNS BIGINT
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE days INTEGER; deleted BIGINT;
BEGIN
  SELECT GREATEST(60, LEAST(3650, value::integer)) INTO days
    FROM public.settings WHERE key='audit_log_retention_days';
  PERFORM set_config('cybercalc.audit_purge','on',true);
  WITH expired AS (
    SELECT * FROM public.audit_log
    WHERE created_at < now()-make_interval(days=>COALESCE(days,60))
    ORDER BY chain_seq LIMIT 10000
  ), archived AS (
    INSERT INTO public.audit_log_archive(
      id,chain_seq,entity_type,entity_id,action,actor_type,user_id,comment_text,
      old_value,new_value,request_id,created_at,prev_hash,row_hash)
    SELECT id,chain_seq,entity_type,entity_id,action,actor_type,user_id,comment_text,
      old_value,new_value,request_id,created_at,prev_hash,row_hash
    FROM expired
    ON CONFLICT (id) DO NOTHING
    RETURNING id
  )
  DELETE FROM public.audit_log WHERE id IN (SELECT id FROM archived);
  GET DIAGNOSTICS deleted = ROW_COUNT;
  PERFORM set_config('cybercalc.audit_purge','off',true);
  RETURN deleted;
END $$;
REVOKE ALL ON FUNCTION purge_expired_audit() FROM PUBLIC;
