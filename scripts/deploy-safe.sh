#!/usr/bin/env bash
# Build while the current revision remains live; restore captured images and
# service environments if startup or revision checks fail. Never undo migrations.
set -Eeuo pipefail
umask 077
command -v jq >/dev/null
deploy_state="$(mktemp -d)"
rollback_file="$deploy_state/rollback.json"
printf '{"services":{}}\n' > "$rollback_file"
previous=false
project="${COMPOSE_PROJECT_NAME:-cybercalculatorminc}"
runtime_services=(backend worker nginx)

# Прерванный `docker compose up` (в том числе ручной) оставляет контейнеры с
# временными именами вида <12 hex>_<проект>-<сервис>-<n>: Compose переименовывает
# старый контейнер перед созданием нового. Такие остатки не работают, но держат
# имена и мешают следующему запуску («removal already in progress»). Удаляются
# только неработающие: живой контейнер под временным именем может оказаться
# единственным рабочим экземпляром.
cleanup_stale_containers() {
  local stale
  stale="$(docker ps -a --no-trunc --format '{{.ID}} {{.Names}}' \
    --filter status=created --filter status=exited --filter status=dead --filter status=removing \
    | awk -v project="$project" '$2 ~ ("^[0-9a-f]{12}_" project "-") { print $1 }')"
  if [[ -n "$stale" ]]; then
    echo "Removing leftover containers from an interrupted deployment:" >&2
    # shellcheck disable=SC2086
    docker rm -f $stale >/dev/null 2>&1 || true
  fi
}
cleanup_stale_containers
previous_services=()
for service in "${runtime_services[@]}"; do
  container="$(docker compose ps -q "$service" | head -n 1)"
  # Образ прежнего экземпляра мог быть удалён (очистка, прерванный запуск):
  # откатываться на него нельзя, и такой сервис в откат не попадает.
  if [[ -n "$container" ]] && docker image inspect "$(docker inspect --format '{{.Image}}' "$container")" >/dev/null 2>&1; then
    previous=true
    previous_services+=("$service")
    docker inspect "$container" | jq --arg service "$service" \
      '.[0] | {services:{($service):{image:.Image,environment:(.Config.Env | map(capture("^(?<key>[^=]+)=(?<value>.*)$")) | from_entries)}}}' > "$deploy_state/$service.json"
    jq -s '.[0] * .[1]' "$rollback_file" "$deploy_state/$service.json" > "$deploy_state/merged.json"
    mv "$deploy_state/merged.json" "$rollback_file"
  fi
done
rollback() {
  result=$?
  trap - ERR
  if [[ "$previous" == true ]]; then
    echo 'Deployment failed. Restoring previous images and environment; database migrations are retained.' >&2
    docker compose -f docker-compose.yml -f "$rollback_file" up -d --no-build --pull never --no-deps "${previous_services[@]}" || true
    for service in "${runtime_services[@]}"; do
      was_running=false
      for previous_service in "${previous_services[@]}"; do
        if [[ "$service" == "$previous_service" ]]; then
          was_running=true
          break
        fi
      done
      if [[ "$was_running" == false ]]; then
        docker compose stop "$service" >/dev/null 2>&1 || true
      fi
    done
  fi
  echo "Diagnostic state retained at $deploy_state (contains secrets; owner-only access)." >&2
  exit "$result"
}
trap rollback ERR
docker compose build --pull

# OPS-11: до любых изменений в работающем стеке сверяем схему базы с деревом
# миграций нового образа. Схема новее образа, изменённая применённая миграция
# или нарушенный порядок — выкладку не начинаем: контейнеры ещё не тронуты,
# откатывать нечего. Первая установка (базы ещё нет) проверку пропускает.
if [[ -n "$(docker compose ps -q db 2>/dev/null | head -n 1)" ]]; then
  if ! docker compose run --rm --no-deps -T --entrypoint /app/upgradecheck migrate schema /app/migrations; then
    trap - ERR
    echo "Schema preflight failed; the running stack was not touched." >&2
    rm -r "$deploy_state"
    exit 1
  fi
fi
# Одна повторная попытка после очистки остатков: сбой при пересоздании
# контейнеров из-за чужого недозавершённого запуска не должен ронять выкладку.
if ! docker compose up -d --remove-orphans --wait --wait-timeout 180; then
  echo "First start failed; cleaning up leftovers and retrying once." >&2
  cleanup_stale_containers
  sleep 5
  docker compose up -d --remove-orphans --wait --wait-timeout 180
fi

# A local curl succeeds even when Docker publishes nginx only on 127.0.0.1.
# Production is opened through the VM floating IP, so verify the real host
# binding before declaring the deployment healthy.
nginx_container="$(docker compose ps -q nginx)"
[[ -n "$nginx_container" ]]
published_addresses="$(docker inspect "$nginx_container" | jq -r '.[0].NetworkSettings.Ports["80/tcp"][]?.HostIp')"
if ! grep -Fxq '0.0.0.0' <<< "$published_addresses"; then
  echo "nginx port 80 is not published on all IPv4 interfaces; addresses: ${published_addresses:-none}" >&2
  false
fi

for _ in {1..30}; do
  version="$(curl --max-time 5 -fsS "http://127.0.0.1:${HTTP_PORT:-8080}/version.txt" 2>/dev/null || true)"
  if [[ "$version" == "${APP_VERSION:-dev}" ]] && curl --max-time 5 -fsS "http://127.0.0.1:${HTTP_PORT:-8080}/api/ready" >/dev/null; then
    trap - ERR
    # Exact mktemp-owned paths only; no application volumes are removed.
    rm -r "$deploy_state"
    echo "Verified revision $version"
    exit 0
  fi
  sleep 2
done
false # invokes rollback trap
