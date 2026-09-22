-- BASE-10: every audit record has one actor/action/entity/old/new/request-id
-- contract. Historical rows receive stable synthetic correlation identifiers.
ALTER TABLE audit_log ADD COLUMN actor_type TEXT;
ALTER TABLE audit_log ADD COLUMN request_id TEXT;

UPDATE audit_log
SET actor_type = CASE WHEN user_id IS NULL THEN 'system' ELSE 'user' END,
    request_id = 'legacy:' || id::text
WHERE actor_type IS NULL OR request_id IS NULL;

ALTER TABLE audit_log ALTER COLUMN actor_type SET NOT NULL;
ALTER TABLE audit_log ALTER COLUMN request_id SET NOT NULL;

ALTER TABLE audit_log ADD CONSTRAINT audit_log_actor_type_check
  CHECK (actor_type IN ('user','system'));
ALTER TABLE audit_log ADD CONSTRAINT audit_log_actor_identity_check
  CHECK ((actor_type='user' AND user_id IS NOT NULL)
      OR (actor_type='system' AND user_id IS NULL));
ALTER TABLE audit_log ADD CONSTRAINT audit_log_request_id_check
  CHECK (length(btrim(request_id)) BETWEEN 1 AND 128);

CREATE INDEX idx_audit_log_request_id ON audit_log(request_id);
