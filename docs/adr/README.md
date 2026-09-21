# Каталог архитектурных решений ТЗ 4.4

Каталог фиксирует решения и открытые вопросы из раздела 2
[`TZ_4_4_PARALLEL_PLAN.md`](../TZ_4_4_PARALLEL_PLAN.md). Наличие ADR не означает,
что решение уже принято: это единая точка, где записаны статус, владелец,
варианты, критерий закрытия и затрагиваемые пакеты.

## Статусы

- `PROPOSED` — вопрос сформулирован, решение ещё не утверждено.
- `ACCEPTED` — решение принято и может использоваться как контракт реализации.
- `SUPERSEDED` — заменено более новым ADR.
- `REJECTED` — рассмотрено и отклонено.

Менять `PROPOSED` на `ACCEPTED` вправе указанный владелец решения. Коммит с
реализацией не считается автоматическим утверждением продуктового или
юридического решения.

## Реестр

| ADR | Тема | Статус | Владелец решения | Backlog |
|---|---|---|---|---|
| [ADR-01](ADR-01-three-percent-base.md) | Семантика базы 3% | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-02](ADR-02-calendar-policy.md) | Календарь таймеров | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-03](ADR-03-top-it-exemption.md) | Исключение ТОП-ИТ по п. 22 | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-04](ADR-04-primary-database.md) | Основная СУБД | ACCEPTED | Технический руководитель | — |
| [ADR-05](ADR-05-frontend-delivery.md) | Доставка frontend | PROPOSED | Технический руководитель | Technical |
| [ADR-06](ADR-06-mfa-policy.md) | Обязательность 2FA | PROPOSED | Security owner | Technical/Security |
| [ADR-07](ADR-07-retention-policy.md) | Хранение вложений и аудита | PROPOSED | Security owner + юрист | Technical/Legal |
| [ADR-08](ADR-08-report-templates.md) | Эталонные формы отчётности | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-09](ADR-09-mvp-cryptography.md) | Криптография MVP | PROPOSED | Security owner | Technical/Security |
| [ADR-10](ADR-10-legacy-migration.md) | Миграция `entries` | PROPOSED | Data owner | Technical |
| [ADR-11](ADR-11-mandatory-activity-scope.md) | Гранулярность Видов 1 и 3 | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-12](ADR-12-activity-seven-formula.md) | Формула Вида 7 | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-13](ADR-13-internship-agreement.md) | Стажировка «вне соглашения» | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-14](ADR-14-top-it-percent-denominator.md) | Знаменатель процентов ТОП-ИТ | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-15](ADR-15-risk-assertions.md) | Ручные и вычисляемые риски | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-16](ADR-16-it-experience.md) | ИТ-стаж за пять лет | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-17](ADR-17-legal-specialist-authority.md) | Полномочия LEGAL_SPECIALIST | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-18](ADR-18-compliance-states.md) | Readiness, eligibility, approval | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-19](ADR-19-counted-amount.md) | Источник зачётной суммы | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-20](ADR-20-snapshot-cutoff.md) | Snapshot cutoff и timezone | PROPOSED | Владелец продукта + юрист | Product/Legal |
| [ADR-21](ADR-21-product-brand.md) | Название и эмблема продукта | PROPOSED | Владелец продукта + юрист | Product/Legal |

## Backlog решений

### Product/Legal

ADR-01–ADR-03, ADR-08 и ADR-11–ADR-21 должны быть закрыты владельцем
продукта совместно с юристом до приёмки зависящих от них модулей.

### Technical/Security

ADR-05–ADR-07, ADR-09 и ADR-10 закрываются технической командой. ADR-07
дополнительно требует юридического подтверждения сроков хранения. ADR-04 уже
принят на основании действующего [`DECISIONS.md`](../../DECISIONS.md).

## Порядок работы

1. В ADR добавляются проверяемые варианты и ссылки на нормативный источник.
2. Владелец фиксирует решение, дату и основание, меняет статус на `ACCEPTED`.
3. Реализация и тесты ссылаются на ADR по номеру.
4. Изменение принятого решения оформляется новым ADR; старый получает статус
   `SUPERSEDED` и ссылку на замену.
