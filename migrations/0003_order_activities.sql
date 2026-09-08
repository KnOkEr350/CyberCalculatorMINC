-- Приведение справочника активностей к восьми видам мероприятий Методики.
-- Внутренний код it_clubs сохранён для совместимости с уже созданными
-- записями, но наименование и расчёт соответствуют мероприятию № 6.

UPDATE activity_categories
SET name = 'Дополнительные общеобразовательные программы для учащихся 5–11 классов',
    obligation = 'variable',
    audience_scope = ARRAY['school']::TEXT[]
WHERE code = 'it_clubs';

INSERT INTO activity_categories (code, name, obligation, audience_scope)
VALUES (
    'teacher_training',
    'Повышение квалификации учителей информатики',
    'variable',
    ARRAY['school']::TEXT[]
)
ON CONFLICT (code) DO UPDATE
SET name = EXCLUDED.name,
    obligation = EXCLUDED.obligation,
    audience_scope = EXCLUDED.audience_scope;

UPDATE activity_categories
SET name = 'Софинансирование образовательных программ ТОП ИТ / ТОП ИИ',
    obligation = 'variable'
WHERE code = 'top_it';

UPDATE activity_categories
SET name = 'Доступ к образовательному контенту на цифровых платформах',
    obligation = 'variable',
    audience_scope = ARRAY['school']::TEXT[]
WHERE code = 'edu_content';

UPDATE activity_categories
SET name = 'Мероприятия по Решению Минцифры',
    obligation = 'variable'
WHERE code = 'minc_decision';
