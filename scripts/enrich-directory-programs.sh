#!/usr/bin/env bash
set -Eeuo pipefail
# Fetch exact program codes from each university/branch's education page.
docker compose run --rm --no-deps -T \
  -e "DIRECTORY_ENRICH_LIMIT=${DIRECTORY_ENRICH_LIMIT:-0}" \
  backend enrich-programs
