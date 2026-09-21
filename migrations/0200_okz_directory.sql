-- DATA-05: versioned Russian Classification of Occupations (OKZ).
CREATE TABLE okz_catalog_versions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    version         TEXT NOT NULL UNIQUE CHECK (length(trim(version)) BETWEEN 1 AND 64),
    source_name     TEXT NOT NULL CHECK (length(trim(source_name)) BETWEEN 2 AND 300),
    source_url      TEXT NOT NULL DEFAULT '',
    effective_on    DATE NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('active', 'archived')),
    imported_by     UUID NOT NULL REFERENCES users(id),
    imported_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX okz_catalog_one_active
    ON okz_catalog_versions ((status)) WHERE status = 'active';

CREATE TABLE okz_occupations (
    version_id      UUID NOT NULL REFERENCES okz_catalog_versions(id) ON DELETE CASCADE,
    code            TEXT NOT NULL CHECK (code ~ '^[0-9]{1,4}$'),
    level           SMALLINT GENERATED ALWAYS AS (length(code)) STORED,
    parent_code     TEXT GENERATED ALWAYS AS (NULLIF(left(code, length(code) - 1), '')) STORED,
    name            TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 2 AND 500),
    PRIMARY KEY (version_id, code),
    CONSTRAINT okz_level_range CHECK (level BETWEEN 1 AND 4),
    CONSTRAINT okz_parent_fk FOREIGN KEY (version_id, parent_code)
        REFERENCES okz_occupations(version_id, code)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX okz_occupations_version_level_code
    ON okz_occupations(version_id, level, code);
CREATE INDEX okz_catalog_versions_imported_at
    ON okz_catalog_versions(imported_at DESC);

COMMENT ON TABLE okz_catalog_versions IS 'Версии ОК 010-2014 (МСКЗ-08); одновременно активна только одна версия';
COMMENT ON TABLE okz_occupations IS 'Иерархические группировки ОКЗ: 1–4 цифровых разряда';
