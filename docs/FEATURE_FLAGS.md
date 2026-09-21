# Feature flags

Новые модули ТЗ 4.4 поставляются выключенными и включаются в конфигурации VM без пересборки образов.

## Конфигурация

В `.env` используются два независимых списка через запятую:

```dotenv
BACKEND_FEATURE_FLAGS=teachers,oop_rpd
FRONTEND_FEATURE_FLAGS=teachers
```

В этом примере API преподавателей и ООП/РПД разрешены на backend, но пользователям показан только экран преподавателей. После изменения `.env` необходимо пересоздать `backend` и `worker`. Frontend получает актуальный набор через `GET /api/features`; пересборка статического контейнера не требуется.

Все флаги выключены по умолчанию. Неизвестное имя считается ошибкой конфигурации и блокирует запуск процесса. Значение `all` включает все известные флаги и допускается только на тестовом стенде. Отключённый backend-модуль не регистрирует свои маршруты и отвечает `404`; frontend при недоступности конфигурации скрывает все опциональные экраны.

## Реестр

| Флаг | Пакет/экран |
|---|---|
| `dashboard_v44` | Дашборд ТЗ 4.4 |
| `partners_v44` | Партнёры ТЗ 4.4 |
| `teachers` | Преподаватели |
| `oop_rpd` | ООП/РПД |
| `internships` | Стажировки |
| `practice` | Практика |
| `top_it_ai` | ТОП-ИТ/ИИ |
| `schools` | Школы |
| `ministry_decision` | Решение Минцифры |
| `reporting_v44` | Отчётность ТЗ 4.4 |
| `settings_v44` | Настройки ТЗ 4.4 |

## Подключение backend-модуля

Модуль передаётся в composition root через условный registrar:

```go
routing.When(cfg.BackendFeatureFlags.Enabled(featureflags.Teachers), teachers.New(db))
```

Регистратор и его тесты разрабатываются внутри предметного модуля. Изменение списка в `server/routes.go` выполняет интеграционный поток.

## Подключение frontend-экрана

Descriptor экрана получает стабильное имя флага, после чего registry фильтруется общей функцией:

```js
const visibleScreens = CyberCalcFeatures.filter([
  { id: "dashboard", render: renderDashboard },
  { id: "teachers", featureFlag: "teachers", render: renderTeachers },
]);
```

Перед построением первой навигации модульный frontend ожидает `CyberCalcFeatures.ready`. Текущие legacy-экраны не имеют `featureFlag` и продолжают работать независимо от загрузки конфигурации.

