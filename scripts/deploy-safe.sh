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
runtime_services=(backend worker frontend nginx)
previous_services=()
for service in "${runtime_services[@]}"; do
  container="$(docker compose ps -q "$service" | head -n 1)"
  if [[ -n "$container" ]]; then
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
    docker compose -f docker-compose.yml -f "$rollback_file" up -d --no-build --no-deps "${previous_services[@]}" || true
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
docker compose up -d --remove-orphans --wait --wait-timeout 180

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

for attempt in {1..30}; do
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
