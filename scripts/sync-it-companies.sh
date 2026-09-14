#!/usr/bin/env bash
set -Eeuo pipefail
# Import the entire CSV/XLSX registry snapshot using runtime DB permissions.
# Reads a local file, or downloads a supplied official HTTPS snapshot URL.
if [[ $# -ne 1 ]]; then
  echo 'Usage: bash scripts/sync-it-companies.sh registry.csv|registry.xlsx|https://official-source/registry.csv' >&2
  exit 2
fi
registry_input="$1"
if [[ "$registry_input" == https://* ]]; then
  registry_tmp="$(mktemp -d)"
  trap 'rm -f -- "$registry_tmp/snapshot"; rmdir -- "$registry_tmp"' EXIT
  curl --fail --show-error --location --proto '=https' --proto-redir '=https' \
    --max-time 180 --max-filesize 33554432 "$registry_input" -o "$registry_tmp/snapshot"
  registry_input="$registry_tmp/snapshot"
fi
if [[ ! -f "$registry_input" || ! -r "$registry_input" ]]; then
  echo 'Registry file does not exist or is unreadable' >&2
  exit 2
fi
docker compose run --rm --no-deps -T backend sync-it-companies < "$registry_input"
