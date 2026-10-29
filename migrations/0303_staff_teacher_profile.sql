ALTER TABLE staff_members
  ADD COLUMN is_teacher BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN archived_at TIMESTAMPTZ;

UPDATE staff_members SET is_teacher = TRUE;

DROP INDEX staff_members_company_fio_unique;
CREATE UNIQUE INDEX staff_members_company_fio_unique
  ON staff_members(it_company_id, lower(fio)) WHERE archived_at IS NULL;

CREATE TABLE staff_member_directions (
  staff_member_id UUID NOT NULL REFERENCES staff_members(id) ON DELETE CASCADE,
  specialty_code TEXT NOT NULL CHECK (specialty_code ~ '^[0-9]{2}\.[0-9]{2}\.[0-9]{2}$'),
  PRIMARY KEY (staff_member_id, specialty_code)
);

CREATE TABLE staff_member_partners (
  staff_member_id UUID NOT NULL REFERENCES staff_members(id) ON DELETE CASCADE,
  partner_id UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
  PRIMARY KEY (staff_member_id, partner_id)
);
CREATE INDEX staff_member_partners_partner_idx ON staff_member_partners(partner_id);
