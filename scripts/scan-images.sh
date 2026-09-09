#!/usr/bin/env bash
set -Eeuo pipefail
mkdir -p security-artifacts
# Trivy must already be installed by the pinned CI setup action.
for service in backend frontend nginx db; do
  container="$(docker compose ps -q "$service" | head -n 1)"
  [[ -n "$container" ]] || { echo "Missing running service: $service" >&2; exit 1; }
  image="$(docker inspect --format '{{.Image}}' "$container")"
  trivy image --ignore-unfixed --severity HIGH,CRITICAL --exit-code 1 "$image"
  trivy image --format cyclonedx --output "security-artifacts/$service.cdx.json" "$image"
done
