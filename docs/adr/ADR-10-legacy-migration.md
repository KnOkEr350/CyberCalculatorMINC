# ADR-10: Миграция существующих `entries`

- Статус: `PROPOSED`
- Владелец решения: Data owner
- Backlog: Technical
- Блокирует: BASE-11, DATA-13 и доменные backfill-задачи

## Контекст

ТЗ 4.4 требует типизированные таблицы, тогда как MVP хранит разнородные поля в
`entries.payload`. Нужен переход без потери данных и без остановки текущих
пользовательских сценариев.

## Варианты

1. Big-bang migration с коротким maintenance window.
2. Dual-write, backfill, сверка, переключение чтения и удаление legacy позже.
3. Read-through adapter с ленивой миграцией записей.

## Критерий принятия

Зафиксированы stable IDs, mapping всех legacy-полей, обработка неоднозначных
строк, reconciliation report, cutover, rollback и срок удаления dual-write.
