-- AUDIT-01: журнал аудита только дополняется. До этой миграции неизменяемость
-- держалась договорённостью: purge_expired_audit() был единственным местом,
-- где удалялись записи, но сама таблица принимала любой UPDATE и DELETE.
-- Теперь запрет живёт в БД: изменение записи запрещено всегда, удаление —
-- всюду, кроме очистки по сроку хранения, которая помечает себя флагом
-- транзакции.
CREATE OR REPLACE FUNCTION prevent_audit_log_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_setting('cybercalc.audit_purge', true) = 'on' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'audit log is append-only';
END $$;

CREATE TRIGGER audit_log_append_only
BEFORE UPDATE OR DELETE ON audit_log
FOR EACH ROW EXECUTE FUNCTION prevent_audit_log_mutation();

-- Очистка по сроку хранения остаётся единственным разрешённым удалением.
-- Флаг ставится локально в транзакции функции и не переживает её.
CREATE OR REPLACE FUNCTION purge_expired_audit() RETURNS BIGINT
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE days INTEGER; deleted BIGINT;
BEGIN
  SELECT GREATEST(60, LEAST(3650, value::integer)) INTO days
    FROM public.settings WHERE key='audit_log_retention_days';
  PERFORM set_config('cybercalc.audit_purge','on',true);
  DELETE FROM public.audit_log WHERE id IN (
    SELECT id FROM public.audit_log WHERE created_at < now()-make_interval(days=>COALESCE(days,60))
    ORDER BY created_at LIMIT 10000
  );
  GET DIAGNOSTICS deleted = ROW_COUNT;
  PERFORM set_config('cybercalc.audit_purge','off',true);
  RETURN deleted;
END $$;
REVOKE ALL ON FUNCTION purge_expired_audit() FROM PUBLIC;
