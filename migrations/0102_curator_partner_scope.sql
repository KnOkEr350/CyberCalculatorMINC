-- Organization curators belong to an IT company and may also be bound to one
-- educational partner. Other profiles keep the original single-assignment
-- invariant.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_single_entity_assignment;

ALTER TABLE users ADD CONSTRAINT users_single_entity_assignment CHECK (
  partner_id IS NULL OR it_company_id IS NULL
  OR (entity_type='organization' AND role='curator')
);
