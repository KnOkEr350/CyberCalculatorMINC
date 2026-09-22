-- DATA-08 / TCH-03 / TCH-06: typed employee profiles and teaching payouts.
CREATE TABLE staff_members (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    it_company_id                 UUID NOT NULL REFERENCES accredited_it_companies(id) ON DELETE CASCADE,
    fio                           TEXT NOT NULL CHECK (length(trim(fio)) BETWEEN 3 AND 200),
    company_position              TEXT NOT NULL CHECK (length(trim(company_position)) BETWEEN 2 AND 200),
    company_department            TEXT NOT NULL DEFAULT '' CHECK (length(company_department) <= 200),
    okz_version_id                UUID NOT NULL REFERENCES okz_catalog_versions(id) ON DELETE RESTRICT,
    okz_code                      TEXT NOT NULL,
    it_experience_days            INTEGER NOT NULL DEFAULT 0 CHECK (it_experience_days BETWEEN 0 AND 1827),
    experience_document_reference TEXT NOT NULL DEFAULT '' CHECK (length(experience_document_reference) <= 1000),
    record_status                 TEXT NOT NULL DEFAULT 'unconfirmed_by_admin'
                                  CHECK (record_status IN ('confirmed','unconfirmed_by_admin')),
    created_by                    UUID REFERENCES users(id),
    updated_by                    UUID REFERENCES users(id),
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT staff_members_okz_fk FOREIGN KEY (okz_version_id,okz_code)
        REFERENCES okz_occupations(version_id,code) ON DELETE RESTRICT,
    CONSTRAINT staff_members_confirmation_check CHECK (
        record_status <> 'confirmed' OR
        (it_experience_days >= 365 AND length(trim(experience_document_reference)) > 0)
    )
);

CREATE UNIQUE INDEX staff_members_company_fio_unique
    ON staff_members(it_company_id,lower(fio));
CREATE INDEX staff_members_company_status_idx
    ON staff_members(it_company_id,record_status,lower(fio));
CREATE INDEX staff_members_okz_idx
    ON staff_members(okz_version_id,okz_code);

ALTER TABLE entries
    ADD COLUMN staff_member_id UUID REFERENCES staff_members(id) ON DELETE RESTRICT;
CREATE INDEX entries_staff_member_idx ON entries(staff_member_id)
    WHERE staff_member_id IS NOT NULL;

CREATE FUNCTION validate_teaching_staff_member() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    staff_company UUID;
BEGIN
    IF NEW.staff_member_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.category_code <> 'teachers' THEN
        RAISE EXCEPTION 'staff member can be linked only to a teaching entry';
    END IF;
    SELECT it_company_id INTO staff_company FROM staff_members WHERE id=NEW.staff_member_id;
    IF staff_company IS NULL OR staff_company <> NEW.it_company_id THEN
        RAISE EXCEPTION 'staff member belongs to another IT company';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER entries_validate_teaching_staff
BEFORE INSERT OR UPDATE OF category_code,it_company_id,staff_member_id ON entries
FOR EACH ROW EXECUTE FUNCTION validate_teaching_staff_member();

CREATE TABLE teaching_payouts (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    teaching_activity_id     UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    target_quarter           TEXT NOT NULL CHECK (target_quarter IN ('Q1','Q2','Q3','Q4')),
    target_year              INTEGER NOT NULL CHECK (target_year BETWEEN 2000 AND 2100),
    planned_compensation_rub NUMERIC(18,2) NOT NULL DEFAULT 0 CHECK (planned_compensation_rub >= 0),
    is_fully_paid            BOOLEAN NOT NULL DEFAULT FALSE,
    payout_date              DATE,
    payout_order_num         TEXT NOT NULL DEFAULT '' CHECK (length(payout_order_num) <= 200),
    payout_scan_file         TEXT NOT NULL DEFAULT '' CHECK (length(payout_scan_file) <= 1000),
    finance_officer_id       UUID REFERENCES users(id) ON DELETE RESTRICT,
    created_by               UUID NOT NULL REFERENCES users(id),
    updated_by               UUID REFERENCES users(id),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (teaching_activity_id,target_quarter,target_year),
    CONSTRAINT teaching_payout_paid_details_check CHECK (
        NOT is_fully_paid OR
        (payout_date IS NOT NULL AND length(trim(payout_order_num)) > 0 AND finance_officer_id IS NOT NULL)
    )
);

CREATE INDEX teaching_payouts_year_quarter_idx
    ON teaching_payouts(target_year,target_quarter);

CREATE FUNCTION validate_teaching_payout_entry() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    activity_category TEXT;
BEGIN
    SELECT category_code INTO activity_category FROM entries WHERE id=NEW.teaching_activity_id;
    IF activity_category IS NULL THEN
        RAISE EXCEPTION 'teaching activity not found';
    END IF;
    IF activity_category <> 'teachers' THEN
        RAISE EXCEPTION 'payout can be linked only to a teaching activity';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER teaching_payouts_validate_entry
BEFORE INSERT OR UPDATE OF teaching_activity_id ON teaching_payouts
FOR EACH ROW EXECUTE FUNCTION validate_teaching_payout_entry();

COMMENT ON TABLE staff_members IS 'Verified IT-company employees used by teaching activities';
COMMENT ON TABLE teaching_payouts IS 'Quarterly compensation schedule maintained by financial specialists';
