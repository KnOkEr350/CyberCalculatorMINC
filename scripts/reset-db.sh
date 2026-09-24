#!/usr/bin/env bash
# Полностью пересоздаёт базу PostgreSQL, не удаляя volume с загрузками.
# Перед удалением обязательно создаёт резервную копию базы и uploads.
set -Eeuo pipefail
umask 077

usage() {
  cat <<'EOF'
Использование:
  bash scripts/reset-db.sh <точное-имя-базы>

Пример:
  bash scripts/reset-db.sh cybercalc

Скрипт удалит все данные выбранной БД, заново применит миграции и создаст
первоначального администратора из ADMIN_BOOTSTRAP_EMAIL/ADMIN_BOOTSTRAP_PASSWORD.
Volume uploads_data не удаляется.
EOF
}

if [[ $# -ne 1 || "$1" == "-h" || "$1" == "--help" ]]; then
  usage
  [[ $# -eq 1 ]] && exit 0
  exit 2
fi

expected_db="$1"
if [[ ! "$expected_db" =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "Отказ: недопустимое имя базы: $expected_db" >&2
  exit 2
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"
cd "$repo_root"

command -v docker >/dev/null 2>&1 || {
  echo "Отказ: команда docker не найдена" >&2
  exit 1
}
docker compose version >/dev/null 2>&1 || {
  echo "Отказ: docker compose недоступен" >&2
  exit 1
}
[[ -f docker-compose.yml ]] || {
  echo "Отказ: docker-compose.yml не найден в $repo_root" >&2
  exit 1
}

echo "Проверяю контейнер PostgreSQL…"
docker compose up -d db

actual_db="$(docker compose exec -T db sh -ceu 'printf %s "$POSTGRES_DB"')"
db_user="$(docker compose exec -T db sh -ceu 'printf %s "$POSTGRES_USER"')"
if [[ "$actual_db" != "$expected_db" ]]; then
  echo "Отказ: вы указали '$expected_db', но контейнер использует '$actual_db'." >&2
  exit 1
fi

cat <<EOF

ВНИМАНИЕ: будут безвозвратно удалены все данные базы:
  база:         $actual_db
  владелец:     $db_user
  compose-файл: $repo_root/docker-compose.yml

Перед удалением будет создан backup. Загруженные файлы останутся на месте.
EOF

if [[ ! -t 0 ]]; then
  echo "Отказ: подтверждение нужно ввести вручную в интерактивном терминале." >&2
  exit 1
fi
read -r -p "Для подтверждения введите точное имя базы ($actual_db): " confirmation
if [[ "$confirmation" != "$actual_db" ]]; then
  echo "Отменено: имя базы не совпало."
  exit 1
fi

echo "Создаю обязательную резервную копию…"
bash "$script_dir/backup.sh"

services_stopped=false
on_exit() {
  exit_code=$?
  if [[ $exit_code -ne 0 && "$services_stopped" == "true" ]]; then
    echo "Сброс прерван. Приложение оставлено остановленным; PostgreSQL остаётся запущенным." >&2
    echo "Проверьте: docker compose logs --tail=200 migrate db" >&2
  fi
  exit "$exit_code"
}
trap on_exit EXIT

echo "Останавливаю HTTP-сервис и фоновые задачи…"
docker compose stop nginx backend worker
services_stopped=true

echo "Пересоздаю базу $actual_db…"
docker compose exec -T -e RESET_EXPECTED_DB="$expected_db" db sh -ceu '
  if [ "$POSTGRES_DB" != "$RESET_EXPECTED_DB" ]; then
    echo "Отказ внутри контейнера: имя базы изменилось" >&2
    exit 1
  fi
  exec psql \
    -v ON_ERROR_STOP=1 \
    -v db="$POSTGRES_DB" \
    -v owner="$POSTGRES_USER" \
    -U "$POSTGRES_USER" \
    -d postgres
' <<'SQL'
DROP DATABASE :"db" WITH (FORCE);
CREATE DATABASE :"db" OWNER :"owner";
SQL

echo "Применяю миграции и создаю первоначального администратора…"
docker compose run --rm migrate

echo "Запускаю приложение…"
docker compose up -d --no-build --remove-orphans
services_stopped=false

echo "Проверяю готовность API…"
ready=false
for _ in $(seq 1 60); do
  if docker compose exec -T nginx wget -q -O /dev/null http://127.0.0.1/api/ready 2>/dev/null; then
    ready=true
    break
  fi
  sleep 2
done

if [[ "$ready" != "true" ]]; then
  echo "База пересоздана, но API не вышел в ready за 120 секунд." >&2
  echo "Проверьте: docker compose logs --tail=200 migrate backend nginx" >&2
  exit 1
fi

echo "Готово: база '$actual_db' очищена, миграции применены, API отвечает."
echo "Uploads не удалялись. Данные первого входа возьмите из .env."
