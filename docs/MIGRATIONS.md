# Правила SQL-миграций ТЗ 4.4

Все новые миграции имеют имя `NNNN_lower_snake_case.sql`, регистрируются в
`migrations/checksums.sha256` и после слияния никогда не редактируются. Для
исправления уже применённой миграции создаётся следующая миграция.

## Диапазоны владельцев

| Диапазон | Область |
|---|---|
| 0100–0199 | platform/security |
| 0200–0299 | organizations/directories |
| 0300–0399 | teaching |
| 0400–0499 | internship/practice |
| 0500–0599 | OOP/RPD |
| 0600–0699 | TOP-IT |
| 0700–0799 | schools/Ministry decisions |
| 0800–0899 | reports/workflow/snapshots |
| 0900–0999 | crypto/storage/audit |

Миграции `0001–0022` помечены `legacy` и заморожены. Два файла с историческим
номером `0004` сохраняются без переименования: их имена уже могли попасть в
`schema_migrations`. Новые дубли номеров запрещены.

## Добавление миграции

1. Выбрать свободный номер в диапазоне своего потока.
2. Добавить SQL-файл. Миграция должна быть безопасна для существующей БД.
3. Рассчитать SHA-256 исходных байтов и добавить строку с режимом `active` в
   `migrations/checksums.sha256`.
4. Выполнить проверку:

   ```bash
   cd backend
   go run ./cmd/migrationcheck ../migrations ../migrations/checksums.sha256
   go test ./internal/migrationcheck ./internal/dbx
   ```

5. В PR указать владельца диапазона, сценарий upgrade и rollback приложения.

CI проверяет реестр до unit-тестов. Во время запуска `RunMigrations` повторно
сравнивает SHA-256 с таблицей `schema_migrations`, поэтому изменение уже
применённого файла останавливает развёртывание.
