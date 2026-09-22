-- BASE-13: immutable registry of official normative sources and revision diffs.
CREATE TABLE normative_trusted_hosts (
    host        TEXT PRIMARY KEY CHECK (host = lower(host) AND host !~ '[/@: ]'),
    description TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO normative_trusted_hosts(host,description) VALUES
    ('publication.pravo.gov.ru','Официальное опубликование правовых актов'),
    ('pravo.gov.ru','Официальный интернет-портал правовой информации'),
    ('digital.gov.ru','Минцифры России'),
    ('adm.digital.gov.ru','Официальное файловое хранилище Минцифры России');

CREATE TABLE normative_sources (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    act_code          TEXT NOT NULL CHECK (act_code ~ '^[A-Z0-9][A-Z0-9._-]{1,63}$'),
    title             TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 3 AND 500),
    revision          TEXT NOT NULL CHECK (length(trim(revision)) BETWEEN 1 AND 100),
    published_on      DATE,
    effective_on      DATE NOT NULL,
    source_url        TEXT NOT NULL CHECK (source_url LIKE 'https://%'),
    source_host       TEXT NOT NULL REFERENCES normative_trusted_hosts(host),
    content_sha256    TEXT NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    content_type      TEXT NOT NULL CHECK (length(content_type) BETWEEN 3 AND 200),
    original_filename TEXT NOT NULL CHECK (length(original_filename) BETWEEN 1 AND 255),
    size_bytes        INTEGER NOT NULL CHECK (size_bytes BETWEEN 1 AND 26214400),
    content_bytes     BYTEA NOT NULL,
    imported_by       UUID NOT NULL REFERENCES users(id),
    imported_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT normative_source_size_matches CHECK (octet_length(content_bytes)=size_bytes),
    UNIQUE(act_code,revision),
    UNIQUE(act_code,effective_on)
);

CREATE INDEX normative_sources_act_timeline_idx
    ON normative_sources(act_code,effective_on DESC,imported_at DESC);

CREATE TABLE normative_revision_diffs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    act_code       TEXT NOT NULL,
    from_source_id UUID NOT NULL REFERENCES normative_sources(id) ON DELETE RESTRICT,
    to_source_id   UUID NOT NULL REFERENCES normative_sources(id) ON DELETE RESTRICT,
    protocol       JSONB NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_source_id<>to_source_id),
    UNIQUE(from_source_id,to_source_id)
);

CREATE INDEX normative_revision_diffs_act_idx
    ON normative_revision_diffs(act_code,created_at DESC);

CREATE FUNCTION reject_normative_registry_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'normative registry rows are immutable';
END;
$$;

CREATE TRIGGER normative_sources_immutable
    BEFORE UPDATE OR DELETE ON normative_sources
    FOR EACH ROW EXECUTE FUNCTION reject_normative_registry_mutation();

CREATE TRIGGER normative_revision_diffs_immutable
    BEFORE UPDATE OR DELETE ON normative_revision_diffs
    FOR EACH ROW EXECUTE FUNCTION reject_normative_registry_mutation();

COMMENT ON TABLE normative_sources IS
    'Immutable official normative source revisions verified by exact URL host and SHA-256';
COMMENT ON TABLE normative_revision_diffs IS
    'Reproducible JSON protocol comparing two immutable normative revisions';
