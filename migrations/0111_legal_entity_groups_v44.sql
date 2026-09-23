-- DATA-10: договор о взаимодействии группы лиц (п. 25⁵), версия 4.4.
--
-- Доля участия дочерней организации — обязательный признак группы: участник
-- группы имеет долю больше 25%. Договор перестаёт быть вечным единственным:
-- действующий договор у ИТ-организации один, но расторгнутый остаётся в
-- истории, и на его место можно завести новый.
ALTER TABLE legal_entity_group_members
  ADD COLUMN share_percent NUMERIC(5,2)
    CHECK (share_percent IS NULL OR (share_percent > 25 AND share_percent <= 100));

ALTER TABLE legal_entity_groups
  ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'terminated')),
  ADD COLUMN terminated_on DATE,
  ADD COLUMN termination_reason TEXT CHECK (termination_reason IS NULL OR length(btrim(termination_reason)) BETWEEN 1 AND 2000),
  -- Реквизит прикреплённого скана договора; сам файл хранится в общем хранилище.
  ADD COLUMN document_reference TEXT CHECK (document_reference IS NULL OR length(btrim(document_reference)) BETWEEN 1 AND 1000),
  ADD CONSTRAINT legal_entity_groups_termination_complete CHECK (
    (status = 'active' AND terminated_on IS NULL AND termination_reason IS NULL)
    OR (status = 'terminated' AND terminated_on IS NOT NULL AND termination_reason IS NOT NULL));

ALTER TABLE legal_entity_groups
  DROP CONSTRAINT legal_entity_groups_it_company_id_key,
  DROP CONSTRAINT legal_entity_groups_it_company_id_name_key;

-- Действующий договор один на ИТ-организацию; расторгнутых может быть сколько угодно.
CREATE UNIQUE INDEX legal_entity_groups_one_active_idx ON legal_entity_groups (it_company_id) WHERE status = 'active';
CREATE INDEX legal_entity_groups_history_idx ON legal_entity_groups (it_company_id, created_at DESC);

-- ИТ-организация входит только в одну группу — но в одну действующую: участие в
-- расторгнутой группе остаётся в истории и новому договору не мешает. Прежний
-- уникальный индекс по ИНН не различал их, поэтому его заменяет проверка,
-- учитывающая статус группы.
DROP INDEX legal_entity_group_member_single_it_group;

CREATE OR REPLACE FUNCTION legal_entity_member_single_active_group() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NOT NEW.is_it_organization THEN RETURN NEW; END IF;
  -- Два одновременных договора с одной организацией сериализуются по ИНН.
  PERFORM pg_advisory_xact_lock(hashtextextended('legal_entity_member:' || NEW.inn, 0));
  IF EXISTS (
    SELECT 1 FROM public.legal_entity_group_members m
    JOIN public.legal_entity_groups g ON g.id = m.group_id
    WHERE m.inn = NEW.inn AND m.is_it_organization AND m.group_id <> NEW.group_id AND g.status = 'active'
  ) THEN
    RAISE EXCEPTION 'IT organization already belongs to another active group' USING ERRCODE = '23505';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER legal_entity_member_single_active_group_trg
BEFORE INSERT OR UPDATE OF inn, is_it_organization, group_id ON legal_entity_group_members
FOR EACH ROW EXECUTE FUNCTION legal_entity_member_single_active_group();
