-- DATA-02: история изменений соглашения и закреплённый куратор.
--
-- Каждое изменение соглашения — самого, сторон, ответственных или перечня видов
-- мероприятий — оставляет полный снимок состояния. Снимки пишет БД по
-- отложенным триггерам в конце транзакции, поэтому история полна для любого
-- пути записи (интерфейс, импорт, ручной SQL), а многократная правка в одной
-- транзакции даёт один снимок финального состояния.
ALTER TABLE agreements ADD COLUMN curator_id UUID REFERENCES users(id);

-- Куратор соглашения — куратор организации той же ИТ-компании.
CREATE OR REPLACE FUNCTION agreements_curator_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE curator_role TEXT; curator_entity TEXT; curator_company UUID;
BEGIN
  IF NEW.curator_id IS NULL THEN RETURN NEW; END IF;
  SELECT role, COALESCE(entity_type, ''), it_company_id INTO curator_role, curator_entity, curator_company
    FROM public.users WHERE id = NEW.curator_id;
  IF curator_role IS DISTINCT FROM 'curator' OR curator_entity <> 'organization'
     OR curator_company IS DISTINCT FROM NEW.it_company_id THEN
    RAISE EXCEPTION 'agreement curator must be a curator of the same IT company';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER agreements_curator_guard_trg
BEFORE INSERT OR UPDATE OF curator_id, it_company_id ON agreements
FOR EACH ROW EXECUTE FUNCTION agreements_curator_guard();

CREATE TABLE agreement_revisions (
  id BIGSERIAL PRIMARY KEY,
  agreement_id UUID NOT NULL REFERENCES agreements(id) ON DELETE CASCADE,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  changed_by UUID REFERENCES users(id),
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  snapshot JSONB NOT NULL,
  UNIQUE (agreement_id, revision)
);

-- История только дополняется; удалить её можно лишь каскадом вместе с соглашением.
CREATE OR REPLACE FUNCTION agreement_revisions_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN RETURN OLD; END IF;
  RAISE EXCEPTION 'agreement revisions are append-only';
END $$;
CREATE TRIGGER agreement_revisions_append_only
BEFORE UPDATE OR DELETE ON agreement_revisions
FOR EACH ROW EXECUTE FUNCTION agreement_revisions_guard();

-- Полное состояние соглашения без служебных полей, которые меняются без смысла.
CREATE OR REPLACE FUNCTION agreement_snapshot(target uuid) RETURNS jsonb
LANGUAGE sql STABLE AS $$
  SELECT to_jsonb(a) - 'updated_at' - 'updated_by' - 'created_at' - 'created_by'
    || jsonb_build_object(
      'partner_ids', COALESCE((SELECT jsonb_agg(p.partner_id::text ORDER BY p.is_primary DESC, p.partner_id)
        FROM public.agreement_partners p WHERE p.agreement_id = a.id), '[]'::jsonb),
      'responsible_people', COALESCE((SELECT jsonb_agg(jsonb_build_object('party', r.party, 'full_name', r.full_name,
          'position', r.position, 'email', r.email, 'phone', r.phone) ORDER BY r.party, r.full_name)
        FROM public.agreement_responsible_people r WHERE r.agreement_id = a.id), '[]'::jsonb),
      'activity_codes', COALESCE((SELECT jsonb_agg(q.category_code ORDER BY q.category_code)
        FROM public.agreement_activity_requirements q WHERE q.agreement_id = a.id), '[]'::jsonb))
  FROM public.agreements a WHERE a.id = target
$$;

CREATE OR REPLACE FUNCTION agreement_record_revision(target uuid) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE current_snapshot jsonb; last_snapshot jsonb; next_revision INTEGER; actor UUID;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('agreement_revision:' || target::text, 0));
  current_snapshot := public.agreement_snapshot(target);
  IF current_snapshot IS NULL THEN RETURN; END IF; -- соглашение удалено
  SELECT snapshot, revision INTO last_snapshot, next_revision
    FROM public.agreement_revisions WHERE agreement_id = target ORDER BY revision DESC LIMIT 1;
  IF last_snapshot IS NOT DISTINCT FROM current_snapshot THEN RETURN; END IF;
  SELECT COALESCE(updated_by, created_by) INTO actor FROM public.agreements WHERE id = target;
  INSERT INTO public.agreement_revisions(agreement_id, revision, changed_by, snapshot)
  VALUES (target, COALESCE(next_revision, 0) + 1, actor, current_snapshot);
END $$;

CREATE OR REPLACE FUNCTION agreement_revision_trigger() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_TABLE_NAME = 'agreements' THEN
    PERFORM public.agreement_record_revision(NEW.id);
  ELSIF TG_OP = 'DELETE' THEN
    PERFORM public.agreement_record_revision(OLD.agreement_id);
  ELSE
    PERFORM public.agreement_record_revision(NEW.agreement_id);
  END IF;
  RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER agreements_revision_trg AFTER INSERT OR UPDATE ON agreements
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agreement_revision_trigger();
CREATE CONSTRAINT TRIGGER agreement_partners_revision_trg AFTER INSERT OR UPDATE OR DELETE ON agreement_partners
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agreement_revision_trigger();
CREATE CONSTRAINT TRIGGER agreement_people_revision_trg AFTER INSERT OR UPDATE OR DELETE ON agreement_responsible_people
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agreement_revision_trigger();
CREATE CONSTRAINT TRIGGER agreement_activities_revision_trg AFTER INSERT OR UPDATE OR DELETE ON agreement_activity_requirements
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agreement_revision_trigger();

-- Существующие соглашения получают первую редакцию: с неё начинается история.
SELECT agreement_record_revision(id) FROM agreements;
