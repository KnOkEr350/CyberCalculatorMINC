#!/usr/bin/env bash
# INTG-05: полный регрессионный прогон одной командой.
#
# Скрипт повторяет те же проверки, что и CI, и в том же порядке, поэтому
# расхождение «локально зелено, в CI красно» становится видно до отправки.
# Каждая проверка выполняется до конца: скрипт не останавливается на первой
# ошибке, а печатает итоговую сводку — иначе вторая проблема обнаружится только
# на следующем прогоне.
#
# Использование:
#   scripts/verify.sh                 # всё, что не требует базы
#   TEST_DATABASE_DSN=... scripts/verify.sh   # включая интеграционные тесты
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

FAILED=""
PASSED=0
SKIPPED=""

run() {
  local name="$1"; shift
  printf '\n\033[1m=== %s ===\033[0m\n' "$name"
  if "$@"; then
    PASSED=$((PASSED + 1))
  else
    FAILED="${FAILED}  - ${name}
"
  fi
}

skip() {
  SKIPPED="${SKIPPED}  - ${1}: ${2}
"
  printf '\n\033[1m=== %s ===\033[0m\nпропущено: %s\n' "$1" "$2"
}

gofmt_clean() {
  local dirty
  dirty="$(gofmt -l backend tests)"
  if [ -n "$dirty" ]; then
    echo "не отформатировано:"
    echo "$dirty"
    return 1
  fi
  echo "форматирование в порядке"
}

run "Форматирование Go"        gofmt_clean
run "Сборка backend"           bash -c 'cd backend && go build ./...'
run "go vet backend"           bash -c 'cd backend && go vet ./...'
run "go vet tests"             bash -c 'cd tests && go vet ./...'
run "Реестр миграций"          bash -c 'cd backend && go run ./cmd/migrationcheck ../migrations ../migrations/checksums.sha256'
run "Контракт API"             bash -c 'cd backend && go test ./internal/apicontract/'
run "Фронтенд"                 node frontend/check.mjs

# Линтеры CI. staticcheck ставится тем же способом, что и в конвейере.
run "staticcheck backend"      bash -c 'cd backend && go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...'
run "staticcheck tests"        bash -c 'cd tests && go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...'

if command -v semgrep >/dev/null 2>&1; then
  run "Semgrep (severity ERROR)" semgrep scan --error --metrics=off --severity ERROR \
    --config p/default --config p/owasp-top-ten --quiet .
else
  skip "Semgrep" "semgrep не установлен"
fi

if [ -n "${TEST_DATABASE_DSN:-}" ]; then
  export TEST_MIGRATIONS_DIR="${TEST_MIGRATIONS_DIR:-$ROOT/migrations}"
  # Проверка наследуемой схемы требует пустой базы и выполняется отдельным
  # шагом, как и в CI. В общем прогоне она пропускается, иначе второй запуск
  # пришёлся бы на уже обновлённую базу.
  run "Backend с детектором гонок" env -u TEST_MIGRATION_DATABASE_DSN bash -c 'cd backend && go test -count=1 -race ./...'
  run "Сквозные тесты"             bash -c 'cd tests && go test -count=1 -race -coverpkg=cybercalc/... -covermode=atomic ./...'
  if [ -n "${TEST_MIGRATION_DATABASE_DSN:-}" ]; then
    run "Обновление с наследуемой схемы" bash -c \
      'cd backend && go test -count=1 -race -run "^TestWorkspaceMigrationPreservesLegacyData$" ./internal/dbx'
  else
    skip "Обновление с наследуемой схемы" "не задан TEST_MIGRATION_DATABASE_DSN"
  fi
else
  skip "Тесты с базой" "не задан TEST_DATABASE_DSN"
fi

printf '\n\033[1m=== Итог ===\033[0m\n'
printf 'пройдено проверок: %d\n' "$PASSED"
if [ -n "$SKIPPED" ]; then
  printf 'пропущено:\n%s' "$SKIPPED"
fi
if [ -n "$FAILED" ]; then
  printf '\033[31mне пройдено:\033[0m\n%s' "$FAILED"
  exit 1
fi
printf '\033[32mвсе проверки пройдены\033[0m\n'
