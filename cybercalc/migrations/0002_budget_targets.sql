-- Дашборд "Пульс проекта": поле "Общие затраты, руб. (3% от сэкономленных
-- льгот)" вводится вручную на отчётный год (см. Приказ, Приложение 5,
-- Таблица 1 — правило "не менее 3% от объёма сэкономленных средств").
CREATE TABLE budget_targets (
    report_year       INTEGER PRIMARY KEY,
    target_amount_rub NUMERIC(16,2) NOT NULL,
    updated_by        UUID REFERENCES users(id),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
