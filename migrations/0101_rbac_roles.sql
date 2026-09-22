-- Replace the three coarse legacy roles with the eight roles required by TZ
-- 4.4. Existing privileges are mapped conservatively.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;

UPDATE users SET role=CASE role
  WHEN 'admin' THEN 'super_admin'
  WHEN 'moderator' THEN 'org_admin'
  WHEN 'user' THEN 'curator'
  ELSE role
END;

ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN (
  'super_admin','holding_admin','org_admin','curator','hr_specialist',
  'financial_specialist','legal_specialist','auditor_viewer'
));

CREATE TABLE user_partner_assignments (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  partner_id UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
  valid_from DATE NOT NULL DEFAULT CURRENT_DATE,
  valid_until DATE,
  assigned_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(user_id,partner_id),
  CHECK(valid_until IS NULL OR valid_until>=valid_from)
);

INSERT INTO user_partner_assignments(user_id,partner_id,valid_from)
SELECT id,partner_id,created_at::date FROM users WHERE partner_id IS NOT NULL
ON CONFLICT DO NOTHING;

CREATE INDEX user_partner_assignments_partner_idx
  ON user_partner_assignments(partner_id,user_id);
