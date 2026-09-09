#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
set -o noclobber
if [[ -e .env ]]; then echo '.env already exists; existing credentials were preserved.'; exit 1; fi
command -v openssl >/dev/null
{
  echo 'APP_ENV=development'
  echo 'DB_USER=cybercalc'
  echo "DB_PASSWORD=$(openssl rand -hex 24)"
  echo 'DB_NAME=cybercalc'
  echo 'DB_RUNTIME_USER=cybercalc_app'
  echo "DB_RUNTIME_PASSWORD=$(openssl rand -hex 24)"
  echo 'HTTP_BIND=127.0.0.1'
  echo 'HTTP_PORT=8080'
  echo 'BACKEND_REPLICAS=2'
  echo 'ADMIN_BOOTSTRAP_EMAIL=admin@minc.local'
  echo "ADMIN_BOOTSTRAP_PASSWORD=$(openssl rand -hex 24)"
  echo "MFA_ENCRYPTION_KEY=$(openssl rand -base64 32)"
  echo 'PUBLIC_URL='
  echo 'CLAMAV_ADDRESS='
  echo 'SESSION_TTL_HOURS=12'
  echo 'UPLOAD_QUOTA_BYTES=1073741824'
} > .env
echo '.env created with unique credentials and owner-only permissions. Read the bootstrap password locally from .env; do not commit it.'
