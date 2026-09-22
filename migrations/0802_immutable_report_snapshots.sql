-- Immutable, tenant-scoped source for the "Fact as of 1 May" columns.
CREATE TABLE report_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  it_company_id UUID NOT NULL REFERENCES accredited_it_companies(id),
  report_year INTEGER NOT NULL CHECK(report_year BETWEEN 2000 AND 2100),
  snapshot_date DATE NOT NULL,
  captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  schema_version INTEGER NOT NULL DEFAULT 1 CHECK(schema_version=1),
  payload JSONB NOT NULL CHECK(jsonb_typeof(payload)='object'),
  payload_bytes BYTEA NOT NULL,
  payload_sha256 CHAR(64) NOT NULL CHECK(payload_sha256 ~ '^[0-9a-f]{64}$'),
  sealed_by UUID NOT NULL REFERENCES users(id),
  UNIQUE(it_company_id,report_year),
  CHECK(snapshot_date=make_date(report_year,5,1)),
  CHECK(octet_length(payload_bytes)>0)
);

CREATE INDEX report_snapshots_company_date_idx
  ON report_snapshots(it_company_id,snapshot_date DESC);

CREATE OR REPLACE FUNCTION prevent_report_snapshot_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'report snapshots are immutable';
END $$;

CREATE TRIGGER report_snapshots_immutable_update
BEFORE UPDATE OR DELETE ON report_snapshots
FOR EACH ROW EXECUTE FUNCTION prevent_report_snapshot_mutation();
