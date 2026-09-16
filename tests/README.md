# Тесты backend

В этой папке собраны внешние проверки публичного поведения backend:

- контракт и защита HTTP API;
- авторизация, session-cookie, MFA и регрессии конкурентного входа;
- разграничение доступа к плану, факту, организациям и вложениям;
- отдельный сценарий `admin + edu_institution`;
- расчёты по ставкам Приказа Минцифры и точное округление денежных сумм;
- импорт, экспорт, workflow отчётов и ключевые связи справочников.

Быстрые тесты не требуют PostgreSQL:

```bash
cd tests
go test ./...
```

`TestWorkspaceIntegration` автоматически пропускается без тестовой базы. Для
полного прогона нужна отдельная база с именем `workspace_test`; подключать тесты
к рабочей базе запрещено самой проверкой:

```bash
cd tests
TEST_DATABASE_DSN='host=127.0.0.1 port=5432 user=workspace_test password=workspace_test dbname=workspace_test sslmode=disable' \
TEST_MIGRATIONS_DIR="$PWD/../migrations" \
go test -count=1 -run TestWorkspaceIntegration ./...
```

Внутренние unit-тесты, использующие приватные функции пакетов, остаются в
`backend/internal`: их перенос потребовал бы раскрыть детали реализации и
ухудшил бы изоляцию кода. Полный локальный прогон выполняется двумя командами:

```bash
(cd backend && go test ./...)
(cd tests && go test ./...)
```
