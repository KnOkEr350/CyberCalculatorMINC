-- SEC-08: sliding idle timeout is independent from the absolute session TTL.
ALTER TABLE sessions
  ADD COLUMN last_activity_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX sessions_last_activity_idx ON sessions(last_activity_at);

INSERT INTO settings(key,value)
VALUES('session_idle_timeout_minutes','30')
ON CONFLICT(key) DO NOTHING;

ALTER TABLE settings ADD CONSTRAINT settings_session_idle_timeout_valid CHECK (
  CASE WHEN key='session_idle_timeout_minutes'
    THEN CASE WHEN value ~ '^[0-9]+$' THEN value::integer BETWEEN 5 AND 1440 ELSE FALSE END
    ELSE TRUE
  END
);
