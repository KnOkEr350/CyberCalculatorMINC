#!/usr/bin/env bash
# Run from the repository root against the disposable CI Compose project.
set -Eeuo pipefail

: "${COMPOSE_PROJECT_NAME:?COMPOSE_PROJECT_NAME is required}"

mkdir -p security-artifacts
sudo chown -R 1000:1000 security-artifacts
set +e
docker run --rm \
  --network "${COMPOSE_PROJECT_NAME}_application" \
  --volume "$PWD/security-artifacts:/zap/wrk:rw" \
  ghcr.io/zaproxy/zaproxy:2.17.0@sha256:781a2bdaea47324e7bab583e2263f21d257b0aee61ed51521a5be45f5f5081ef \
  zap-full-scan.py -t http://nginx -m 1 -T 10 -I \
  -z "-config ascan.maxScanDurationInMins=5 -config ascan.maxRuleDurationInMins=1" \
  -J zap-report.json -r zap-report.html
zap_status=$?
set -e
host_uid="$(id -u)"
host_gid="$(id -g)"
sudo chown -R "${host_uid}:${host_gid}" security-artifacts
[[ "$zap_status" == "0" ]]
jq --exit-status '[.site[]?.alerts[]? | select((.riskcode | tonumber) >= 3)] | length == 0' \
  security-artifacts/zap-report.json > /dev/null
