#!/usr/bin/env bash
set -Eeuo pipefail

# Reuses the deployed backend image and its restricted runtime DB credentials.
# Optional tuning: DIRECTORY_ENRICH_LIMIT and DIRECTORY_ENRICH_DELAY_MS.
COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-cybercalculatorminc}" \
  docker compose run --rm \
    -e "DIRECTORY_ENRICH_LIMIT=${DIRECTORY_ENRICH_LIMIT:-0}" \
    -e "DIRECTORY_ENRICH_DELAY_MS=${DIRECTORY_ENRICH_DELAY_MS:-3000}" \
    backend enrich-directory
