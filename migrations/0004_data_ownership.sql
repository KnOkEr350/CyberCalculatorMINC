-- Целевые суммы, как и записи плана/факта, принадлежат конкретной
-- учётной записи. Старые строки закрепляются за пользователем, который
-- последним их изменил; это сохраняет существующие значения.
ALTER TABLE budget_targets ADD COLUMN owner_user_id UUID REFERENCES users(id);

UPDATE budget_targets
SET owner_user_id = COALESCE(
    updated_by,
    (SELECT id FROM users WHERE role = 'admin' ORDER BY created_at LIMIT 1)
)
WHERE owner_user_id IS NULL;

DELETE FROM budget_targets WHERE owner_user_id IS NULL;

ALTER TABLE budget_targets ALTER COLUMN owner_user_id SET NOT NULL;
ALTER TABLE budget_targets DROP CONSTRAINT budget_targets_pkey;
ALTER TABLE budget_targets ADD PRIMARY KEY (report_year, owner_user_id);

CREATE INDEX idx_entries_owner_period ON entries(created_by, partner_id, period_type, report_year);
