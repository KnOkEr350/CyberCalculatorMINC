-- CyberCalculatorMINC — начальная схема БД (PostgreSQL)
-- Соответствует ТЗ "Калькулятор затрат по Приказу МЦ"

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

-- ---------------------------------------------------------------------------
-- Пользователи и роли
-- ---------------------------------------------------------------------------
-- role: 'admin'  — сотрудник Минцифры/оператора калькулятора, видит всё, ведёт логи, настраивает retention
-- role: 'user'   — представитель партнёра (сам решает, за кого он отчитывается: entity_type)
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,       -- salt$hex(PBKDF2-HMAC-SHA256)
    full_name       TEXT NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('admin','user')),
    -- Выбор "кто он": организация (ИТ-организация, обязанная соглашением) или вуз/СПО (образовательная организация-партнёр)
    entity_type     TEXT CHECK (entity_type IN ('organization','edu_institution')),
    partner_id      UUID, -- FK добавляется ниже, после создания partners
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Партнёры инициативы (раздел "Партнеры Инициативы")
-- ---------------------------------------------------------------------------
CREATE TABLE partners (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    partner_kind    TEXT NOT NULL CHECK (partner_kind IN ('vuz','kolledj','school')), -- Вуз / Колледж / Общеобразовательное учреждение
    agreement_date  DATE,
    agreement_number TEXT,
    other_agreement TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE users
    ADD CONSTRAINT fk_users_partner FOREIGN KEY (partner_id) REFERENCES partners(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- Справочник категорий активностей (7 категорий из ТЗ)
-- ---------------------------------------------------------------------------
CREATE TABLE activity_categories (
    code            TEXT PRIMARY KEY,     -- 'teachers','ood_rpd','internship','employment_practice','top_it','it_clubs','edu_content'
    name            TEXT NOT NULL,
    obligation      TEXT NOT NULL CHECK (obligation IN ('mandatory','variable')), -- О / В
    audience_scope  TEXT[] NOT NULL       -- допустимые аудитории: vuz/kolledj/school
);

INSERT INTO activity_categories (code, name, obligation, audience_scope) VALUES
 ('teachers',            'Преподаватели-практики',                                   'mandatory', ARRAY['vuz','kolledj']),
 ('ood_rpd',              'ООП и РПД',                                                'mandatory', ARRAY['vuz','kolledj']),
 ('internship',           'Стажировки',                                              'variable',  ARRAY['vuz','kolledj']),
 ('employment_practice',  'Практика с трудоустройством',                             'variable',  ARRAY['vuz','kolledj']),
 ('top_it',               'Софинансирование программ ТОП ИТ, ТОП ИИ',                'variable',  ARRAY['vuz','kolledj']),
 ('minc_decision',        'Реализация по Решению Минцифры',                          'variable',  ARRAY['vuz','kolledj','school']),
 ('it_clubs',             'ИТ-кружки для школьников 5-11 классов',                   'variable',  ARRAY['school']),
 ('edu_content',          'Образовательный контент для школьников и учителей',       'variable',  ARRAY['school']);

-- ---------------------------------------------------------------------------
-- Записи калькулятора (план/факт) — по категориям, поля категории лежат в payload
-- ---------------------------------------------------------------------------
CREATE TABLE entries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_code   TEXT NOT NULL REFERENCES activity_categories(code),
    partner_id      UUID REFERENCES partners(id),
    period_type     TEXT NOT NULL CHECK (period_type IN ('plan','fact')),
    report_year     INTEGER NOT NULL,
    audience        TEXT NOT NULL CHECK (audience IN ('vuz','kolledj','school')),
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,   -- поля конкретной категории (Ак.ч., ФИО, курс и т.д.)
    amount_rub      NUMERIC(16,2) NOT NULL DEFAULT 0,     -- рассчитанная сумма затрат
    created_by      UUID NOT NULL REFERENCES users(id),
    updated_by      UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_entries_category ON entries(category_code);
CREATE INDEX idx_entries_partner ON entries(partner_id);
CREATE INDEX idx_entries_period ON entries(period_type, report_year);

-- ---------------------------------------------------------------------------
-- Вложения-подтверждения к фактическим записям (документ, хранится N дней, по умолчанию год)
-- ---------------------------------------------------------------------------
CREATE TABLE attachments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id        UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    file_name       TEXT NOT NULL,
    storage_path    TEXT NOT NULL,
    content_type    TEXT,
    size_bytes      BIGINT NOT NULL,
    uploaded_by     UUID NOT NULL REFERENCES users(id),
    uploaded_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    retention_expires_at TIMESTAMPTZ NOT NULL -- вычисляется при загрузке из settings.attachment_retention_days
);

CREATE INDEX idx_attachments_entry ON attachments(entry_id);
CREATE INDEX idx_attachments_retention ON attachments(retention_expires_at);

-- ---------------------------------------------------------------------------
-- Комментарии к правкам записи (обязателен при редактировании плана/факта)
-- ---------------------------------------------------------------------------
CREATE TABLE entry_comments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id        UUID NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id),
    comment_text    TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_entry_comments_entry ON entry_comments(entry_id);

-- ---------------------------------------------------------------------------
-- Журнал аудита (видно только админам). Хранится 2 месяца — чистится job'ом.
-- ---------------------------------------------------------------------------
CREATE TABLE audit_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type     TEXT NOT NULL,       -- 'entry','attachment','user','partner','settings'
    entity_id       UUID,
    action          TEXT NOT NULL,       -- 'create','update','delete','login','upload','settings_change'
    user_id         UUID REFERENCES users(id),
    comment_text    TEXT,                -- комментарий пользователя при правке (если применимо)
    old_value       JSONB,
    new_value       JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_created ON audit_log(created_at);
CREATE INDEX idx_audit_log_entity ON audit_log(entity_type, entity_id);

-- ---------------------------------------------------------------------------
-- Сессии (cookie-based auth без сторонних библиотек)
-- ---------------------------------------------------------------------------
CREATE TABLE sessions (
    token           TEXT PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- ---------------------------------------------------------------------------
-- Настройки, регулируемые администратором
-- ---------------------------------------------------------------------------
CREATE TABLE settings (
    key             TEXT PRIMARY KEY,
    value           TEXT NOT NULL,
    updated_by      UUID REFERENCES users(id),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO settings (key, value) VALUES
 ('attachment_retention_days', '365'),   -- срок хранения файлов-подтверждений факта (админ может менять)
 ('audit_log_retention_days',  '60');    -- 2 месяца хранения логов (фиксировано в ТЗ, но оставлено настраиваемым)
