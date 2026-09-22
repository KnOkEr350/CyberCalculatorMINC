# CI/CD для Debian VM

Workflow `.github/workflows/ci-cd.yml` запускается при каждом push, pull
request, вручную и по понедельникам для плановой перепроверки баз уязвимостей.
Проверки разделены по назначению и выполняются параллельно:

| Job | Что проверяет |
|---|---|
| `quality` | `gofmt`, `go vet`, `staticcheck`, `actionlint` и `shellcheck` |
| `backend` | unit-тесты backend с Go race detector и отдельным покрытием |
| `frontend` | синтаксис всех JavaScript-модулей и frontend-тесты, найденные автоматически |
| `migrations` | checksum registry и обновление тестовой БД с legacy-схемы с повторным безопасным применением |
| `contract` | соответствие OpenAPI зарегистрированным маршрутам и contract regression tests |
| `golden` | точное сравнение генерируемого OOXML с версионированным эталоном |
| `e2e` | внешние API-, authentication- и workspace-сценарии с настоящим PostgreSQL |
| `dynamic` | fuzzing XLSX-парсера и воспроизводимые CPU/memory-профили benchmark |
| `codeql` | data-flow/SAST анализ Go и JavaScript набором `security-extended` |
| `semgrep` | блокирующие security-правила Semgrep для Go, JavaScript, конфигураций и OWASP Top 10 |
| `supply-chain` | Trivy (включая секреты), `govulncheck` и генерация CycloneDX SBOM |
| `dependency-track` | загрузка готового SBOM на отдельном self-hosted runner рядом с Dependency-Track |
| `container-dast` | сборка изолированного Compose-стенда, сканирование всех образов Trivy и активный DAST через OWASP ZAP |
| `deploy` | безопасное обновление Debian VM и проверка SHA реально запущенной версии |

Шесть модульных gates независимы: ошибка frontend не отменяет backend,
migrations, contract, golden или e2e. PostgreSQL поднимается только для
`migrations` и `e2e`; остальные проверки не ждут сервисный контейнер. Frontend
использует `frontend/check.mjs`, поэтому список модулей и тестов не дублируется
в YAML при добавлении нового экрана.

Основные сценарии входа, ролей, планов и фактов, workflow, импорта, вложений и
отчётов выполняют типизированные Go-тесты. Compose-job дополнительно проверяет
границы nginx и контейнеров, readiness API и worker, tenant-изоляцию, права
модераторов, создание факта и работу вложений в полностью собранном стенде.

Эти проверки реализованы в `tests/compose_smoke*_test.go`: `TestComposeBoundary`
проверяет frontend, API, SHA версии, worker и вход до сканирования ZAP;
`TestComposeWorkspace` после сканирования проверяет создание/редактирование факта,
оба варианта multipart-поля (`files` и `file`), скачивание вложений и права
модераторов. HTTP-запросы идут через опубликованный nginx с отдельными cookie jar
для пользователей. Фикстуры создаются функциями из
`compose_smoke_fixtures_test.go` через `docker compose exec db psql`; порт БД
наружу не открывается.

Обычный `go test ./...` пропускает Compose-тесты. Для запуска на уже поднятом
одноразовом CI-стенде из каталога `tests`:

```bash
COMPOSE_PROJECT_NAME=cybercalc-ci-local \
COMPOSE_SMOKE_URL=http://127.0.0.1:18080 \
APP_VERSION=EXPECTED_BUILT_REVISION \
go test -count=1 -v -timeout 3m -run '^TestCompose' .
```

Стенд должен использовать свежую БД `cybercalc_ci`, владельца `cybercalc_ci` и
bootstrap-пользователя `ci-admin@example.invalid` с паролем
`ci-only-admin-password`, как в job `container-dast`. Перед повторным запуском
сценария workspace пересоздайте только этот одноразовый стенд с чистыми томами:
фикстуры рассчитаны на один прогон. Тесты требуют имя проекта `cybercalc-ci-*`
и сверяют локальный HTTP-порт с опубликованным портом nginx этого проекта.
В артефакт `container-and-zap-reports` входят журналы Go-тестов, а при ошибке —
также состояние контейнеров и их логи, снятые до удаления CI-стенда.

## Почему выбраны именно эти динамические инструменты

Backend написан на чистом Go и production-бинарник собирается с
`CGO_ENABLED=0`. Поэтому Valgrind, AddressSanitizer, gprof, Helgrind и
ThreadSanitizer для C/C++ не дают здесь полезного покрытия. Их задачи закрыты
нативными средствами Go:

- `go test -race` проверяет гонки памяти и конкурентный доступ;
- Go fuzzing мутирует недоверенный XLSX/ZIP/XML-ввод;
- `go test -bench -benchmem` и `pprof` дают CPU, allocation и heap-профили.

Linux `perf` и `strace` зависят от прав и настроек ядра GitHub runner, поэтому
их результаты нестабильны и плохо подходят для обязательного merge-gate.
`DTrace` на Linux runner недоступен. OWASP ZAP используется как практический
application scan/DAST уже собранного стенда; отдельный коммерческий IBM
AppScan не добавлен, поскольку требует лицензии и дублирует этот слой.

## Отчёты безопасности

CI сохраняет артефакты с покрытием, `pprof`, SARIF Semgrep, отчётами ZAP и
CycloneDX SBOM. CodeQL и Semgrep также отправляют результаты в GitHub Code
Scanning. Security actions и Docker-образы закреплены по commit SHA или digest,
чтобы тег стороннего инструмента нельзя было незаметно подменить. Зависимости Go
проверяются `govulncheck` и Trivy, а CycloneDX SBOM отправляется в
Dependency-Track для непрерывного анализа.

Для отправки SBOM в существующий OWASP Dependency-Track настройте:

| Тип | Имя | Значение |
|---|---|---|
| Repository variable | `DTRACK_HOSTNAME` | hostname сервера без `https://` |
| Repository variable | `DTRACK_PROTOCOL` | необязательно: `http` по умолчанию |
| Repository variable | `DTRACK_PORT` | необязательно: `8080` по умолчанию |
| Repository secret | `DTRACK_API_KEY` | API-ключ команды с правами `BOM_UPLOAD` и `PROJECT_CREATION_UPLOAD` |

Без этих двух значений SBOM всё равно создаётся и сохраняется как CI artifact,
но upload в Dependency-Track явно помечается предупреждением.

Загрузка выполняется только для `main` после push, ручного запуска или запуска
по расписанию. Зарегистрируйте на VM с Dependency-Track отдельный repository
runner с label `dependency-track`. Если API опубликован на loopback-интерфейсе
этой же VM, задайте `DTRACK_HOSTNAME=127.0.0.1`. Этому runner не нужен доступ к
Docker: он скачивает созданный GitHub-hosted job артефакт и отправляет SBOM в
локальный API. Не назначайте ему label `cybercalculator-prod`.

## Подготовка production Debian VM

На VM должны быть установлены Git, curl, jq, Docker Engine и Docker Compose plugin:

```bash
git --version
curl --version
jq --version
docker version
docker compose version
```

Пользователь runner должен работать без root и иметь доступ к Docker. В примере
ниже его имя — `github-runner`:

```bash
sudo useradd --create-home --shell /bin/bash github-runner
sudo usermod --append --groups docker github-runner
```

В GitHub откройте `Settings → Actions → Runners → New self-hosted runner`,
выберите Linux и архитектуру VM. Выполните на VM команды скачивания и
регистрации, показанные GitHub. При вызове `config.sh` добавьте label:

```bash
./config.sh \
  --url https://github.com/KnOkEr350/CyberCalculatorMINC \
  --token TOKEN_FROM_GITHUB \
  --labels cybercalculator-prod \
  --unattended
```

Команду нужно выполнять от `github-runner`. После регистрации установите runner
как systemd service из его каталога:

```bash
sudo ./svc.sh install github-runner
sudo ./svc.sh start
sudo ./svc.sh status
```

Runner должен отображаться в GitHub как `Idle` и иметь labels `self-hosted`,
`Linux`, `X64`, `cybercalculator-prod`. Для публичного репозитория не разрешайте
self-hosted runner выполнять jobs из pull requests или сторонних веток. В этом
workflow production runner используется только после push в `main` или ручного
запуска workflow из `main`.

## Production environment и секреты

Создайте GitHub Environment с именем `production`:

`Settings → Environments → New environment → production`

Добавьте в него secrets:

| Secret | Назначение |
|---|---|
| `PROD_DB_USER` | пользователь PostgreSQL |
| `PROD_DB_PASSWORD` | пароль PostgreSQL |
| `PROD_DB_NAME` | имя базы данных |
| `PROD_DB_RUNTIME_PASSWORD` | отдельный сильный пароль ограниченной роли приложения |
| `PROD_ADMIN_BOOTSTRAP_EMAIL` | email первого администратора |
| `PROD_ADMIN_BOOTSTRAP_PASSWORD` | пароль первого администратора |
| `PROD_MFA_ENCRYPTION_KEY` | постоянный Base64-ключ из 32 случайных байт для MFA |

Дополнительно можно создать environment variables:

| Variable | Значение по умолчанию |
|---|---|
| `PROD_HTTP_PORT` | `8080` |
| `PROD_BACKEND_REPLICAS` | `2` |
| `PROD_DB_RUNTIME_USER` | `cybercalc_app` |

Production workflow всегда задаёт `HTTP_BIND=0.0.0.0`, поскольку приложение
доступно через плавающий IP виртуальной машины. Деплой дополнительно проверяет
реальный проброс порта контейнера nginx и завершается с ошибкой, если Docker
опубликовал порт только на `127.0.0.1`.

Workflow передаёт секреты Docker Compose через окружение runner и не сохраняет
deployment `.env` в репозитории. Пока система разрабатывается без домена, CD
запускает её с `APP_ENV=development` по HTTP и не требует `PUBLIC_URL` или
`CLAMAV_ADDRESS`. В этом режиме cookie не имеют production-флага `Secure`, а
загружаемые документы не проходят антивирусную проверку. Перед публичным
production-запуском необходимо вернуть `APP_ENV=production`, настроить HTTPS и
доступный ClamAV. Убедитесь, что `PROD_HTTP_PORT` свободен на VM.

Если на VM уже существует volume PostgreSQL, значения `PROD_DB_USER`,
`PROD_DB_PASSWORD` и `PROD_DB_NAME` должны совпадать с настройками, с которыми
этот volume был создан. Bootstrap-пароль применяется только при создании первого
администратора и не меняет пароль уже существующей учётной записи.

## Первый запуск

После установки runner и настройки environment достаточно отправить изменения
в `main`. Последовательность будет такой:

```text
push → параллельные quality/tests/SAST/SCA/DAST gates → deploy на Debian VM
```

`actions/checkout` обновляет служебную копию в рабочем каталоге runner, обычно
`~/actions-runner/_work/CyberCalculatorMINC/CyberCalculatorMINC`. Отдельный
клон `~/CyberCalculatorMINC` не участвует в автоматическом деплое и поэтому не
обязан обновляться после workflow. Запущенные контейнеры собираются из
служебной копии runner.

Проверить службу runner на VM можно командой:

```bash
sudo journalctl -u 'actions.runner.*' --follow
```

Состояние приложения после CD:

```bash
docker compose -p cybercalculatorminc ps
curl -I http://127.0.0.1:8080
curl http://127.0.0.1:8080/version.txt
```

Последняя команда должна вернуть полный SHA коммита из успешного deploy-job.
При ошибке deploy сценарий отката восстанавливает прежние `backend`, `worker`, `frontend`
и `nginx`; миграции базы назад не откатываются.
