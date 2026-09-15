-- Bind ordinary organization users to an accredited IT company. Educational
-- users continue to use partner_id, which points to their educational partner.
ALTER TABLE users
  ADD COLUMN it_company_id UUID REFERENCES accredited_it_companies(id) ON DELETE SET NULL;

ALTER TABLE users
  ADD CONSTRAINT users_single_entity_assignment
  CHECK (partner_id IS NULL OR it_company_id IS NULL);

CREATE INDEX users_it_company_idx ON users(it_company_id) WHERE it_company_id IS NOT NULL;
