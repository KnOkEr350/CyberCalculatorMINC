# Пакеты ADR-05, DATA-01, DATA-06, DATA-07, DATA-12 и WF-01

## Доставка frontend

SPA встроена в Go-бинарник пакетом `internal/modules/webapp`. В контейнерной
сборке frontend копируется перед компиляцией; отдельный frontend-контейнер не
нужен. Nginx проксирует и `/api/`, и SPA в backend, сохраняя TLS/WAF/rate-limit.

Для локальной синхронизации embedded-копии используется:

```bash
scripts/sync-embedded-frontend.sh
```

Тест `webapp.TestEmbeddedSourcesMatchFrontend` запрещает собрать устаревшую
локальную копию. `index.html` и `version.txt` не кэшируются; остальные пока
нехешированные ресурсы всегда ревалидируются.

## Организации и специальности

- `GET /api/organizations` — единый read-каталог ИТ-компаний, ВО, СПО, школ и
  РОИВ с ИНН/КПП/ОГРН, адресом, аккредитацией и ролью.
- `GET /api/specialties` — активная версия нормализованного справочника.
- `POST /api/specialty-catalogs/import` — атомарный импорт UTF-8 CSV или XLSX.

Импорт справочника требует multipart-поля `version_code`, `title`,
`effective_from`, `normative_source_id`, `file`. Обязательные столбцы файла:
`code`, `education_system`, `title`; необязательный — `qualification_level`.
`education_system` принимает `higher` или `secondary_vocational`.

## Тарифы

- `GET /api/tariffs` выбирает версию по `report_year`, `activity_code` и
  `audience`.
- `POST /api/tariff-versions/import` атомарно загружает новую версию CSV/XLSX.

Обязательные столбцы: `activity_code`, `audience`, `component_code`,
`rate_rub`, `unit_code`. Версия активируется только со ссылкой на неизменяемый
`normative_source_id`. Production-путь расчёта читает ставки из выбранной
версии и сохраняет `tariff_version_id` в записи. Миграционная legacy-версия
имеет `provenance_verified=false` и не допускается для новых расчётов: перед
вводом мероприятий администратор должен импортировать официально сверенную
редакцию.

## Типизированные документы

Вложение хранит owner, нормативный тип, даты действия, signer/certificate/key
metadata, состояние юридической проверки и отдельный legal dispute.

- `PATCH /api/attachments/{id}/metadata` использует поле `version` и возвращает
  `409` при конкурентном изменении.
- `PATCH /api/attachments/{id}/review` фиксирует проверку.
- `PATCH /api/attachments/{id}/dispute` создаёт или снимает юридическое
  сомнение; при создании причина обязательна.

Байты blob этими операциями не изменяются. Каждая мутация попадает в аудит.

## Регламентный workflow

`regulatory_workflows` ведёт три независимых процесса: `agreement`,
`preliminary`, `final`. Статусы: `draft`, `sent`, `in_review`, `rework`,
`resubmitted`, `approved`, `default_approved`, `disputed`.

- `POST /api/regulatory-workflows` создаёт процесс;
- `GET /api/regulatory-workflows` читает его по соглашению, году и типу;
- `PATCH /api/regulatory-workflows/{id}` выполняет разрешённый переход.

Переход требует причины и текущей версии. Конфликт версий возвращает `409`;
запрещённый переход не записывается. История и audit event создаются в той же
транзакции.
