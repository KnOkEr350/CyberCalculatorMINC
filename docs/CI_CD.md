# CI/CD для Debian VM

Workflow `.github/workflows/ci-cd.yml` запускается при каждом push, pull
request и вручную. Он состоит из трёх jobs:

1. `quality` запускает форматирование, линтеры и проверки безопасности.
   Найденные проблемы блокируют production-деплой.
2. `verify` на GitHub-hosted Ubuntu runner выполняет Go-тесты backend и отдельного
   модуля публичных contract-тестов, собирает и
   запускает весь Docker Compose, проверяет frontend, nginx, PostgreSQL,
   миграции, вход, создание и редактирование факта, загрузку и скачивание
   вложений, health-контракты и запуск worker. Ошибка этого job блокирует CD.
3. `deploy` запускается только для `main`, только после успешного `verify` и
   выполняет `docker compose up` на Debian VM через self-hosted runner. После
   пересоздания сервисов сверяет readiness API и версию, которую
   отдаёт frontend, с SHA проверенного коммита.

## Подготовка Debian VM

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
push → quality + verify (параллельно) → deploy на Debian VM
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
