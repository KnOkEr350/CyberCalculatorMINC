ALTER TABLE mentors
  ADD COLUMN last_name TEXT NOT NULL DEFAULT '' CHECK (length(last_name) <= 100),
  ADD COLUMN first_name TEXT NOT NULL DEFAULT '' CHECK (length(first_name) <= 100),
  ADD COLUMN middle_name TEXT NOT NULL DEFAULT '' CHECK (length(middle_name) <= 100),
  ADD COLUMN position TEXT NOT NULL DEFAULT '' CHECK (length(position) <= 200),
  ADD COLUMN archived_at TIMESTAMPTZ,
  ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

UPDATE mentors SET
  last_name = split_part(full_name, ' ', 1),
  first_name = split_part(full_name, ' ', 2),
  middle_name = btrim(substr(full_name, length(split_part(full_name, ' ', 1)) + length(split_part(full_name, ' ', 2)) + 3));

DROP INDEX mentors_partner_name;
CREATE UNIQUE INDEX mentors_partner_name ON mentors(partner_id, lower(full_name)) WHERE archived_at IS NULL;
