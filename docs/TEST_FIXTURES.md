# Общие тестовые фабрики

BASE-09 реализован пакетом `backend/internal/testfixtures`. Пакет доступен
внутренним тестам backend и внешнему модулю `tests` и создаёт согласованные с
актуальной схемой PostgreSQL записи без копирования SQL по пакетам.

## Готовые фабрики

- `CreateITCompany` — аккредитованная ИТ-компания с валидными уникальными ИНН
  и ОГРН;
- `CreateUniversity`, `CreateCollege`, `CreateSchool` — проверенные записи
  справочника ВО, СПО и школ;
- `CreatePartner` — tenant-scoped партнёр выбранной ИТ-компании;
- `CreateRegionalAuthority` — действующий РОИВ;
- `CreateAgreement` — действующее соглашение с ответственными обеих сторон и
  разрешёнными видами мероприятий;
- `CreateUser` — пользователь любой роли и области доступа с известным паролем;
- `CreateScenario` — атомарный связанный набор из одной ИТ-компании, ВО, СПО,
  школы, РОИВ, трёх соглашений и пользователей обеих сторон.

## Использование

```go
fixtures := testfixtures.New(db, t.Name())
scenario, err := fixtures.CreateScenario(context.Background())
if err != nil {
    t.Fatal(err)
}

client.Login(scenario.CompanyAdmin.Email, scenario.CompanyAdmin.Password)
```

В качестве namespace всегда передаётся `t.Name()`. Фабрика нормализует его и
добавляет монотонный номер, поэтому параллельные пакеты не конфликтуют по
email, ИНН, ОГРН, registry ID и номерам соглашений. `CreateScenario` выполняет
весь набор в одной транзакции: при любой ошибке частичные данные не остаются.
Для автоматической очистки тест начинает транзакцию, создаёт фабрику через
`NewTx` и вызывает `tx.Rollback()` в `t.Cleanup`/`defer`.

Интеграционная проверка запускается вместе с backend-тестами при наличии
`TEST_DATABASE_DSN` и `TEST_MIGRATIONS_DIR`. База должна быть одноразовой и
иметь имя с префиксом `workspace_test`.
