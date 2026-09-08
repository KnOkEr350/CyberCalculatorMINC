# CyberCalculatorMINC — калькулятор затрат по Приказу Минцифры

Реализация ТЗ «Калькулятор затрат по Приказу МЦ»: расчёт стоимости
активностей организаций/вузов в рамках соглашений о содействии
образовательным программам, план/факт, админка, логирование изменений,
хранение подтверждающих документов, выгрузка отчётов в xlsx.

## Состав репозитория

```
.
├── backend/                 # Go-бэкенд (единственная зависимость — lib/pq)
│   ├── cmd/server/          # точка входа, роутинг
│   ├── internal/
│   │   ├── auth/            # хеширование паролей (PBKDF2, stdlib) + сессии
│   │   ├── calculators/     # формулы расчёта стоимости по категориям
│   │   ├── config/          # конфиг из переменных окружения
│   │   ├── dbx/              # подключение к БД + миграции
│   │   ├── handlers/        # HTTP-хендлеры
│   │   ├── middleware/      # авторизация/роли
│   │   ├── models/          # доменные структуры
│   │   ├── retention/       # фоновая очистка логов/файлов по срокам
│   │   └── xlsx/            # минимальный xlsx-writer на archive/zip
│   ├── Dockerfile           # multi-stage сборка
│   └── go.mod
├── frontend/                # чистый HTML/CSS/JS без фреймворков
│   ├── Dockerfile           # отдельная multi-stage сборка frontend
│   └── nginx.conf           # раздача собранной статики
├── migrations/              # SQL-миграции (применяются автоматически при старте)
├── nginx/
│   └── nginx.conf           # входная точка и балансировка запросов
├── docker-compose.yml
├── .env.example
├── DECISIONS.md              # обоснование выбора БД, источники формул расчёта
└── README.md
```

## Запуск

```bash
cp .env.example .env
# при необходимости отредактируйте .env (пароли, порт)
docker compose up --build
```

Интерфейс откроется на `http://localhost:8080` (порт настраивается через
`HTTP_PORT`). Внешний трафик принимает nginx: запросы `/api/` балансируются
между репликами backend, остальные запросы направляются во frontend. Число
реплик backend задаётся через `BACKEND_REPLICAS` и по умолчанию равно двум.

Backend и PostgreSQL не публикуют порты наружу и доступны только внутри
docker-сети. Все backend-реплики используют общие PostgreSQL и volume для
загруженных документов. Применение миграций защищено advisory lock, поэтому
параллельный старт реплик безопасен.

При первом запуске, если в базе ещё нет ни одного администратора, автоматически
создаётся учётная запись admin — email/пароль берутся из `ADMIN_BOOTSTRAP_EMAIL`
/ `ADMIN_BOOTSTRAP_PASSWORD` (см. `.env.example`). **Смените пароль сразу после
первого входа** (Админка → Пользователи → создать нового admin → отключить
bootstrap-аккаунт, либо обновить его пароль напрямую в БД).

## Как это соответствует ТЗ

| Требование ТЗ | Где реализовано |
|---|---|
| Выбор БД (postgres/sqlite) | PostgreSQL — обоснование в `DECISIONS.md` |
| Админка, «кто он» (организация/вуз) | `POST /api/auth/entity-type`, экран выбора при первом входе |
| Расчёт стоимости активностей по Приказу | `backend/internal/calculators/*` — по одному файлу на категорию |
| Вкладки план/факт | `period_type` в `entries`, переключатель в UI |
| Внесение суммы в плане | форма записи, `POST /api/entries` |
| Занесение факта + подтверждающий документ, хранение год (регулируется) | `POST /api/entries/{id}/attachments`, `settings.attachment_retention_days` |
| Обязательный комментарий при редактировании + видимость админу | `PUT /api/entries/{id}` требует `comment`, пишется в `entry_comments` и `audit_log` |
| Несколько админов | `role = 'admin'` у произвольного числа пользователей, `POST /api/admin/users` |
| Логи изменений в интерфейсе, хранение 2 месяца | `GET /api/admin/logs`, `backend/internal/retention` (job раз в час) |
| Бэк на Go без сторонних библиотек | весь код на stdlib, единственная зависимость — драйвер `lib/pq` (без него `database/sql` не умеет говорить с Postgres) |
| Всё в compose, Dockerfile multi-stage | `docker-compose.yml`, `backend/Dockerfile`, `frontend/Dockerfile`, nginx-балансировщик |

## API (кратко)

- `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/auth/me`
- `POST /api/auth/entity-type` — выбор «организация / вуз»
- `GET /api/categories` — справочник категорий + поля формы + формула
- `GET/POST /api/partners`
- `GET/POST /api/entries`, `PUT /api/entries/{id}` (требует `comment`)
- `GET/POST /api/entries/{id}/attachments`, `GET /api/attachments/{id}/download`
- `GET /api/dashboard`, `POST /api/dashboard/target`
- `GET /api/reports/export?period_type=&report_year=&category_code=` — xlsx, лист «Сводный для МЦ» + лист на каждого партнёра
- `GET/POST /api/admin/users`, `PATCH /api/admin/users/{id}`
- `GET/POST /api/admin/settings`
- `GET /api/admin/logs`

## Известные ограничения MVP

- Фронтенд — рабочий, но минималистичный (без drag&drop, пагинации таблиц и т.п.); при необходимости легко нарастить поверх текущего API.
- Экспорт xlsx — валидный OOXML без стилей/форматирования ячеек (шрифты, границы), сфокусирован на данных.
- Для категорий, для которых ни в ТЗ, ни в Приказе не задана формула/ставка («Реализация по Решению Минцифры», «ИТ-кружки для школьников», «Образовательный контент»), сумма вводится вручную — см. `DECISIONS.md`.
