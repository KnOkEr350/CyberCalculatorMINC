#!/usr/bin/env bash
# Verify HTTP responses and revision after deployment; does not change application data.
set -Eeuo pipefail

base_url="http://127.0.0.1:${HTTP_PORT}"
for _ in {1..30}; do
  frontend_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "${base_url}/" || true)"
  api_status="$(curl --silent --output /dev/null --write-out '%{http_code}' "${base_url}/api/auth/me" || true)"
  served_version="$(curl --silent --fail "${base_url}/version.txt" 2>/dev/null | tr -d '\r\n' || true)"
  if [[ "$frontend_status" == "200" && "$api_status" == "401" && "$served_version" == "$APP_VERSION" ]]; then
    echo "Revision $APP_VERSION is live"
    exit 0
  fi
  sleep 2
done
echo "Deployment health check failed: expected revision $APP_VERSION, got ${served_version:-nothing}"
exit 1
