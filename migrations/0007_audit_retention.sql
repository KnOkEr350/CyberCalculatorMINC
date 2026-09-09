-- The runtime role cannot delete arbitrary audit records. This narrowly scoped
-- owner function only purges records older than the enforced retention period.
CREATE FUNCTION purge_expired_audit() RETURNS BIGINT
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE days INTEGER; deleted BIGINT;
BEGIN
  SELECT GREATEST(60, LEAST(3650, value::integer)) INTO days
    FROM public.settings WHERE key='audit_log_retention_days';
  DELETE FROM public.audit_log WHERE id IN (
    SELECT id FROM public.audit_log WHERE created_at < now()-make_interval(days=>COALESCE(days,60))
    ORDER BY created_at LIMIT 10000
  );
  GET DIAGNOSTICS deleted = ROW_COUNT;
  RETURN deleted;
END $$;
REVOKE ALL ON FUNCTION purge_expired_audit() FROM PUBLIC;
