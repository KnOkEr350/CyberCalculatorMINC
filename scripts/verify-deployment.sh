#!/usr/bin/env bash
# Verify HTTP responses and revision after deployment; does not change application data.
set -Eeuo pipefail

# При ручной выкладке значения берутся из .env рядом с compose-файлом, как их
# берёт сам docker compose; заданное в окружении имеет приоритет.
if [[ -f .env ]]; then
  for var in HTTP_PORT APP_VERSION BACKEND_FEATURE_FLAGS FRONTEND_FEATURE_FLAGS; do
    if [[ -z "${!var:-}" ]]; then
      value="$(grep -E "^${var}=" .env | tail -n 1 | cut -d= -f2- | tr -d '\r"'"'" || true)"
      [[ -z "$value" ]] || export "$var=$value"
    fi
  done
fi
: "${HTTP_PORT:?HTTP_PORT is required}"
: "${APP_VERSION:?APP_VERSION is required}"

base_url="http://127.0.0.1:${HTTP_PORT}"
for _ in {1..30}; do
  frontend_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "${base_url}/" || true)"
  api_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "${base_url}/api/auth/me" || true)"
  served_version="$(curl --silent --fail "${base_url}/version.txt" 2>/dev/null | tr -d '\r\n' || true)"
  if [[ "$frontend_status" == "200" && "$api_status" == "401" && "$served_version" == "$APP_VERSION" ]]; then
    echo "Revision $APP_VERSION is live"
    break
  fi
  sleep 2
done
if [[ "${frontend_status:-}" != "200" || "${api_status:-}" != "401" || "${served_version:-}" != "$APP_VERSION" ]]; then
  echo "Deployment health check failed: expected revision $APP_VERSION, got ${served_version:-nothing}"
  exit 1
fi

# Работающая ревизия ещё не значит работающие модули: без флагов маршруты ТЗ 4.4
# не подключаются, а интерфейс показывает «Модули не включены». Сверяем то, что
# сервер отдаёт на деле, с тем, что задано в конфигурации выкладки.
# Маршрут модуля без входа отвечает 401; 404 значит, что модуль не подключён.
probe_path() {
  case "$1" in
    dashboard_v44) echo /api/dashboard ;;
    partners_v44) echo /api/partners ;;
    teachers) echo /api/staff-members ;;
    oop_rpd | internships | practice | top_it_ai | schools | ministry_decision) echo /api/entries ;;
    reporting_v44) echo /api/reports/export ;;
    settings_v44) echo /api/admin/users ;;
  esac
}
failed=0
features="$(curl --silent --max-time 10 "${base_url}/api/features" || true)"
for name in $(tr ',' ' ' <<< "${FRONTEND_FEATURE_FLAGS:-}"); do
  [[ "$name" == "all" ]] && continue
  if [[ "$(jq -r --arg n "$name" '.flags[$n] // false' <<< "$features" 2>/dev/null)" != "true" ]]; then
    echo "Feature flag $name is configured for the frontend but /api/features does not report it enabled"
    failed=1
  fi
done
for name in $(tr ',' ' ' <<< "${BACKEND_FEATURE_FLAGS:-}"); do
  [[ "$name" == "all" ]] && continue
  path="$(probe_path "$name")"
  [[ -n "$path" ]] || { echo "No route probe is defined for feature flag $name"; failed=1; continue; }
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' --max-time 10 "${base_url}${path}" || true)"
  if [[ "$status" != "401" ]]; then
    echo "Module $name is enabled but ${path} answered ${status:-nothing} instead of 401 (module not routed)"
    failed=1
  fi
done
if [[ -z "${BACKEND_FEATURE_FLAGS:-}" ]]; then
  echo "warning: BACKEND_FEATURE_FLAGS is empty; ТЗ 4.4 modules are disabled in this deployment" >&2
fi
[[ "$failed" == "0" ]] || exit 1
echo "Feature modules verified"
