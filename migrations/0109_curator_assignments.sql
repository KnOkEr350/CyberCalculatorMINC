-- DATA-09: назначение куратора на партнёра с периодом действия и границей
-- арендатора.
--
-- Таблица user_partner_assignments была с 0101, но ничего не защищала: период
-- нигде не читался, закрепления пересекались, админка стирала историю и
-- вставляла заново, а куратор другой ИТ-компании мог быть закреплён за чужим
-- партнёром. Теперь она — единственный источник того, за кем закреплён
-- куратор, а users.partner_id остаётся производным значением для существующих
-- проверок доступа.
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE OR REPLACE FUNCTION moscow_today() RETURNS date
LANGUAGE sql STABLE AS $$ SELECT (now() AT TIME ZONE 'Europe/Moscow')::date $$;

ALTER TABLE user_partner_assignments
  DROP CONSTRAINT user_partner_assignments_pkey,
  ADD COLUMN id UUID NOT NULL DEFAULT gen_random_uuid(),
  ADD COLUMN it_company_id UUID REFERENCES accredited_it_companies(id),
  ADD COLUMN reason TEXT CHECK (reason IS NULL OR length(btrim(reason)) BETWEEN 1 AND 2000),
  ADD COLUMN revoked_at TIMESTAMPTZ,
  ADD COLUMN revoked_by UUID REFERENCES users(id),
  ADD COLUMN revoke_reason TEXT CHECK (revoke_reason IS NULL OR length(btrim(revoke_reason)) BETWEEN 1 AND 2000);
ALTER TABLE user_partner_assignments ADD PRIMARY KEY (id);

UPDATE user_partner_assignments a SET it_company_id = p.it_company_id
FROM partners p WHERE p.id = a.partner_id;

-- Отозванное закрепление — ошибка назначения или немедленная замена: оно не
-- действует ни в какой день и не мешает новому. Обычное окончание — это
-- valid_until, и история такого периода остаётся действующей.
ALTER TABLE user_partner_assignments
  ADD CONSTRAINT user_partner_assignments_revocation_complete CHECK (
    (revoked_at IS NULL AND revoked_by IS NULL AND revoke_reason IS NULL)
    OR (revoked_at IS NOT NULL AND revoke_reason IS NOT NULL));

-- Куратор закреплён не больше чем за одним партнёром в любой день.
ALTER TABLE user_partner_assignments
  ADD CONSTRAINT user_partner_assignments_one_partner_at_a_time
  EXCLUDE USING gist (user_id WITH =, daterange(valid_from, valid_until, '[]') WITH &&)
  WHERE (revoked_at IS NULL);

CREATE INDEX user_partner_assignments_effective_idx
  ON user_partner_assignments (user_id, valid_from, valid_until) WHERE revoked_at IS NULL;

-- Проверка арендатора и заполнение it_company_id. Куратор организации
-- закрепляется только за партнёром своей ИТ-компании; профиль образовательной
-- организации к арендатору не привязан и проверке не подлежит.
CREATE OR REPLACE FUNCTION user_partner_assignments_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  target_role TEXT; target_entity TEXT; target_company UUID; partner_company UUID;
BEGIN
  IF TG_OP = 'DELETE' THEN
    -- Каскад при удалении пользователя или партнёра допустим; прямое удаление
    -- переписывало бы историю.
    IF pg_trigger_depth() > 1 THEN RETURN OLD; END IF;
    RAISE EXCEPTION 'partner assignments are not deleted; revoke or end them';
  END IF;

  IF TG_OP = 'INSERT' THEN
    SELECT role, COALESCE(entity_type, ''), it_company_id INTO target_role, target_entity, target_company
      FROM public.users WHERE id = NEW.user_id;
    SELECT it_company_id INTO partner_company FROM public.partners WHERE id = NEW.partner_id;
    IF target_entity = 'organization' THEN
      IF target_role <> 'curator' THEN
        RAISE EXCEPTION 'only curators are assigned to partners';
      END IF;
      IF target_company IS NULL OR partner_company IS DISTINCT FROM target_company THEN
        RAISE EXCEPTION 'curator and partner must belong to the same IT company';
      END IF;
    END IF;
    NEW.it_company_id := partner_company;
    RETURN NEW;
  END IF;

  -- Менять можно только конец периода и отзыв; кто, кого и за что закреплён,
  -- остаётся как записано.
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.user_id IS DISTINCT FROM OLD.user_id
     OR NEW.partner_id IS DISTINCT FROM OLD.partner_id OR NEW.valid_from IS DISTINCT FROM OLD.valid_from
     OR NEW.it_company_id IS DISTINCT FROM OLD.it_company_id OR NEW.assigned_by IS DISTINCT FROM OLD.assigned_by
     OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.reason IS DISTINCT FROM OLD.reason THEN
    RAISE EXCEPTION 'partner assignment identity is immutable';
  END IF;
  IF OLD.revoked_at IS NOT NULL AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
     OR NEW.revoked_by IS DISTINCT FROM OLD.revoked_by OR NEW.revoke_reason IS DISTINCT FROM OLD.revoke_reason
     OR NEW.valid_until IS DISTINCT FROM OLD.valid_until) THEN
    RAISE EXCEPTION 'revoked partner assignment is immutable';
  END IF;
  RETURN NEW;
END $$;

CREATE TRIGGER user_partner_assignments_guard_trg
BEFORE INSERT OR UPDATE OR DELETE ON user_partner_assignments
FOR EACH ROW EXECUTE FUNCTION user_partner_assignments_guard();

-- Действующее закрепление куратора на дату.
CREATE OR REPLACE FUNCTION current_partner_of(target uuid, on_date date DEFAULT moscow_today()) RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT partner_id FROM public.user_partner_assignments
  WHERE user_id = target AND revoked_at IS NULL AND valid_from <= on_date
    AND (valid_until IS NULL OR valid_until >= on_date)
  ORDER BY valid_from DESC LIMIT 1
$$;

-- users.partner_id куратора организации — производное значение: его читают
-- существующие проверки доступа. Пересчитывается при любом изменении закреплений.
CREATE OR REPLACE FUNCTION refresh_curator_partner(target uuid) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
  UPDATE public.users SET partner_id = public.current_partner_of(target), updated_at = now()
  WHERE id = target AND role = 'curator' AND entity_type = 'organization'
    AND partner_id IS DISTINCT FROM public.current_partner_of(target);
END $$;

CREATE OR REPLACE FUNCTION user_partner_assignments_refresh() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM public.refresh_curator_partner(COALESCE(NEW.user_id, OLD.user_id));
  RETURN NULL;
END $$;

CREATE TRIGGER user_partner_assignments_refresh_trg
AFTER INSERT OR UPDATE ON user_partner_assignments
FOR EACH ROW EXECUTE FUNCTION user_partner_assignments_refresh();

-- Прямая запись users.partner_id куратора (старые пути, фикстуры, ручной SQL)
-- превращается в закрепление: иначе значение расходилось бы с источником и
-- пропадало при следующем пересчёте. Срабатывает только на прямую запись:
-- пересчёт из триггера выше идёт глубже и сюда не попадает, а плановый
-- пересчёт помечает себя настройкой curators.sync.
CREATE OR REPLACE FUNCTION users_curator_partner_sync() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF pg_trigger_depth() > 1 OR COALESCE(current_setting('curators.sync', true), '') = 'on'
     OR NEW.role <> 'curator' OR COALESCE(NEW.entity_type, '') <> 'organization' THEN
    RETURN NULL;
  END IF;
  IF NEW.partner_id IS NULL THEN
    IF TG_OP = 'UPDATE' AND OLD.partner_id IS NOT NULL THEN
      UPDATE public.user_partner_assignments SET revoked_at = now(),
        revoke_reason = 'снято прямым изменением профиля'
      WHERE user_id = NEW.id AND revoked_at IS NULL AND valid_from <= public.moscow_today()
        AND (valid_until IS NULL OR valid_until >= public.moscow_today());
    END IF;
    RETURN NULL;
  END IF;
  IF public.current_partner_of(NEW.id) IS NOT DISTINCT FROM NEW.partner_id THEN
    RETURN NULL;
  END IF;
  UPDATE public.user_partner_assignments SET revoked_at = now(),
    revoke_reason = 'заменено прямым изменением профиля'
  WHERE user_id = NEW.id AND revoked_at IS NULL AND (valid_until IS NULL OR valid_until >= public.moscow_today());
  INSERT INTO public.user_partner_assignments(user_id, partner_id, valid_from, reason)
  VALUES (NEW.id, NEW.partner_id, public.moscow_today(), 'прямое изменение профиля');
  RETURN NULL;
END $$;

CREATE TRIGGER users_curator_partner_sync_trg
AFTER INSERT OR UPDATE OF partner_id, role, entity_type ON users
FOR EACH ROW EXECUTE FUNCTION users_curator_partner_sync();
