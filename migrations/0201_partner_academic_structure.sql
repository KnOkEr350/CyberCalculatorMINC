-- Typed academic structure for educational partners (TZ 4.4, DATA-03/04).
CREATE TABLE org_units (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id       UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
    parent_unit_id   UUID REFERENCES org_units(id) ON DELETE RESTRICT,
    unit_level_type  TEXT NOT NULL CHECK (unit_level_type IN ('institute','faculty','division','department','laboratory')),
    unit_name        TEXT NOT NULL CHECK (length(trim(unit_name)) BETWEEN 2 AND 300),
    head_fio         TEXT NOT NULL DEFAULT '' CHECK (length(head_fio) <= 200),
    head_position    TEXT NOT NULL DEFAULT '' CHECK (length(head_position) <= 200),
    head_contacts    TEXT NOT NULL DEFAULT '' CHECK (length(head_contacts) <= 500),
    chair_fio        TEXT NOT NULL DEFAULT '' CHECK (length(chair_fio) <= 200),
    chair_contacts   TEXT NOT NULL DEFAULT '' CHECK (length(chair_contacts) <= 500),
    curator_fio      TEXT NOT NULL DEFAULT '' CHECK (length(curator_fio) <= 200),
    curator_contacts TEXT NOT NULL DEFAULT '' CHECK (length(curator_contacts) <= 500),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, partner_id)
);

CREATE UNIQUE INDEX org_units_root_name_unique
    ON org_units(partner_id, lower(unit_name)) WHERE parent_unit_id IS NULL;
CREATE UNIQUE INDEX org_units_child_name_unique
    ON org_units(partner_id, parent_unit_id, lower(unit_name)) WHERE parent_unit_id IS NOT NULL;
CREATE INDEX org_units_partner_parent_idx ON org_units(partner_id, parent_unit_id);

CREATE FUNCTION validate_org_unit_hierarchy() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    ancestor_count INTEGER;
    wrong_partner BOOLEAN;
    contains_self BOOLEAN;
BEGIN
    IF NEW.parent_unit_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.parent_unit_id = NEW.id THEN
        RAISE EXCEPTION 'org unit cannot be its own parent';
    END IF;

    WITH RECURSIVE ancestors AS (
        SELECT id,parent_unit_id,partner_id,1 AS depth
        FROM org_units WHERE id=NEW.parent_unit_id
        UNION ALL
        SELECT parent.id,parent.parent_unit_id,parent.partner_id,child.depth+1
        FROM org_units parent JOIN ancestors child ON parent.id=child.parent_unit_id
        WHERE child.depth < 11
    )
    SELECT count(*),COALESCE(bool_or(partner_id<>NEW.partner_id),FALSE),COALESCE(bool_or(id=NEW.id),FALSE)
      INTO ancestor_count,wrong_partner,contains_self FROM ancestors;

    IF ancestor_count = 0 THEN
        RAISE EXCEPTION 'parent org unit not found';
    END IF;
    IF wrong_partner THEN
        RAISE EXCEPTION 'parent org unit belongs to another partner';
    END IF;
    IF contains_self THEN
        RAISE EXCEPTION 'org unit hierarchy contains a cycle';
    END IF;
    IF ancestor_count >= 10 THEN
        RAISE EXCEPTION 'org unit hierarchy exceeds 10 levels';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER org_units_validate_hierarchy
BEFORE INSERT OR UPDATE OF partner_id,parent_unit_id ON org_units
FOR EACH ROW EXECUTE FUNCTION validate_org_unit_hierarchy();

CREATE TABLE academic_groups (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    partner_id        UUID NOT NULL REFERENCES partners(id) ON DELETE CASCADE,
    unit_id           UUID NOT NULL,
    group_name        TEXT NOT NULL CHECK (length(trim(group_name)) BETWEEN 1 AND 100),
    education_level   TEXT NOT NULL CHECK (education_level IN ('vo_bachelor','vo_master','vo_specialist','spo')),
    course_num        INTEGER NOT NULL CHECK (course_num BETWEEN 1 AND 7),
    current_semester  INTEGER NOT NULL CHECK (current_semester BETWEEN 1 AND 13),
    semester_period   TEXT NOT NULL CHECK (semester_period IN ('spring','autumn')),
    specialty_code    TEXT NOT NULL CHECK (specialty_code ~ '^[0-9]{2}\.[0-9]{2}\.[0-9]{2}$'),
    students_count    INTEGER NOT NULL CHECK (students_count BETWEEN 0 AND 10000),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT academic_groups_unit_partner_fk
        FOREIGN KEY (unit_id,partner_id) REFERENCES org_units(id,partner_id) ON DELETE RESTRICT,
    CONSTRAINT academic_groups_semester_level_check CHECK (
        (education_level='vo_bachelor' AND current_semester BETWEEN 1 AND 8) OR
        (education_level='vo_master' AND current_semester BETWEEN 9 AND 12) OR
        (education_level='vo_specialist' AND current_semester BETWEEN 1 AND 13) OR
        (education_level='spo' AND current_semester BETWEEN 1 AND 10)
    )
);

CREATE UNIQUE INDEX academic_groups_partner_name_unique
    ON academic_groups(partner_id, lower(group_name));
CREATE INDEX academic_groups_partner_unit_idx ON academic_groups(partner_id, unit_id);

CREATE FUNCTION validate_academic_group_partner() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    kind TEXT;
    known_codes TEXT[];
BEGIN
    SELECT p.partner_kind,COALESCE(d.program_codes,'{}'::TEXT[])
      INTO kind,known_codes
      FROM partners p LEFT JOIN education_directory d ON d.id=p.directory_id
     WHERE p.id=NEW.partner_id;
    IF kind IS NULL THEN
        RAISE EXCEPTION 'academic group partner not found';
    END IF;
    IF kind='school' THEN
        RAISE EXCEPTION 'academic groups are available only for HEI and college partners';
    END IF;
    IF kind='kolledj' AND NEW.education_level<>'spo' THEN
        RAISE EXCEPTION 'college group must use SPO education level';
    END IF;
    IF kind='vuz' AND NEW.education_level='spo' THEN
        RAISE EXCEPTION 'HEI group cannot use SPO education level';
    END IF;
    -- A populated partner programme list is authoritative. Legacy partners
    -- with no imported programme list still accept a well-formed Order 27 code.
    IF cardinality(known_codes)>0 AND NOT (NEW.specialty_code=ANY(known_codes)) THEN
        RAISE EXCEPTION 'specialty code is not present in partner programme list';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER academic_groups_validate_partner
BEFORE INSERT OR UPDATE OF partner_id,education_level,specialty_code ON academic_groups
FOR EACH ROW EXECUTE FUNCTION validate_academic_group_partner();
