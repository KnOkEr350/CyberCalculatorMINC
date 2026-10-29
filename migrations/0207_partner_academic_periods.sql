CREATE TABLE partner_academic_periods (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  partner_id UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
  title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 2 AND 200),
  starts_on DATE NOT NULL,
  ends_on DATE NOT NULL,
  created_by UUID REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT partner_academic_periods_range CHECK (ends_on >= starts_on AND ends_on - starts_on <= 366)
);

CREATE UNIQUE INDEX partner_academic_periods_title_unique
  ON partner_academic_periods(partner_id, lower(title));
CREATE INDEX partner_academic_periods_partner_idx
  ON partner_academic_periods(partner_id, starts_on);
