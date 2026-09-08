# CI/CD для Debian VM

Workflow `.github/workflows/ci-cd.yml` запускается при каждом push, pull
request и вручную. Он состоит из трёх jobs:

1. `quality` запускает форматирование, линтеры и проверки безопасности.
   Найденные проблемы видны в GitHub Actions, но не блокируют остальные jobs.
2. `verify` на GitHub-hosted Ubuntu runner выполняет Go-тесты, собирает и
   запускает весь Docker Compose, проверяет frontend, nginx, PostgreSQL,
   миграции, вход и авторизованный API-запрос. Ошибка этого job блокирует CD.
3. `deploy` запускается только для `main`, только после успешного `verify` и
   выполняет `docker compose up` на Debian VM через self-hosted runner.

## Подготовка Debian VM

На VM должны быть установлены Git, curl, Docker Engine и Docker Compose plugin:

```bash
git --version
curl --version
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
| `PROD_ADMIN_BOOTSTRAP_EMAIL` | email первого администратора |
| `PROD_ADMIN_BOOTSTRAP_PASSWORD` | пароль первого администратора |

Дополнительно можно создать environment variables:

| Variable | Значение по умолчанию |
|---|---|
| `PROD_HTTP_PORT` | `8080` |
| `PROD_BACKEND_REPLICAS` | `2` |

Workflow передаёт секреты Docker Compose через окружение runner и не сохраняет
production `.env` в репозитории. Убедитесь, что `PROD_HTTP_PORT` свободен на VM
и разрешён в security group и firewall.

Если на VM уже существует volume PostgreSQL, значения `PROD_DB_USER`,
`PROD_DB_PASSWORD` и `PROD_DB_NAME` должны совпадать с настройками, с которыми
этот volume был создан. Bootstrap-пароль применяется только при создании первого
администратора и не меняет пароль уже существующей учётной записи.

## Первый запуск

После установки runner и настройки environment достаточно отправить изменения
в `main`. Последовательность будет такой:

```text
push → quality (не блокирует) + verify → deploy на Debian VM
```

Проверить службу runner на VM можно командой:

```bash
sudo journalctl -u 'actions.runner.*' --follow
```

Состояние приложения после CD:

```bash
docker compose -p cybercalculatorminc ps
curl -I http://127.0.0.1:8080
```
