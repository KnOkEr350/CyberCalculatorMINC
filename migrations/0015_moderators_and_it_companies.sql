-- Moderator role and the accredited IT-company directory.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users
    ADD CONSTRAINT users_role_check CHECK (role IN ('admin','moderator','user'));

CREATE TABLE accredited_it_companies (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 2 AND 1000),
    inn                  TEXT NOT NULL UNIQUE,
    ogrn                 TEXT NOT NULL UNIQUE,
    accreditation_number TEXT NOT NULL DEFAULT '' CHECK (length(accreditation_number) <= 100),
    accreditation_status TEXT NOT NULL DEFAULT 'active'
        CHECK (accreditation_status IN ('active','suspended','revoked')),
    registry_record_id   TEXT NOT NULL UNIQUE CHECK (length(trim(registry_record_id)) BETWEEN 1 AND 200),
    registry_updated_at  DATE NOT NULL,
    source_url           TEXT NOT NULL,
    notes                TEXT NOT NULL DEFAULT '' CHECK (length(notes) <= 1000),
    created_by           UUID NOT NULL REFERENCES users(id),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX accredited_it_companies_name_idx
    ON accredited_it_companies (lower(name));
CREATE INDEX accredited_it_companies_status_idx
    ON accredited_it_companies (accreditation_status, registry_updated_at DESC);
