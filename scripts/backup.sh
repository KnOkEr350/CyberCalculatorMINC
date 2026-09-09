#!/usr/bin/env bash
# Run on the deployment host. Existing application data is read, never removed.
set -Eeuo pipefail
umask 077
backup_root="${BACKUP_DIR:-$PWD/backups}"
mkdir -p "$backup_root"
backup_path="$(mktemp -d "$backup_root/backup-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX")"
db_container="$(docker compose ps -q db)"
if [[ -z "$db_container" ]]; then
  echo 'No existing database container; first deployment has no data to back up.'
  exit 0
fi
docker compose exec -T db sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$backup_path/database.dump"
test -s "$backup_path/database.dump"
docker compose exec -T db pg_restore --list < "$backup_path/database.dump" > "$backup_path/database.contents"
upload_container="$(docker compose ps -aq uploads-init)"
if [[ -z "$upload_container" ]]; then echo 'Cannot identify uploads volume; deployment stopped.' >&2; exit 1; fi
docker run --rm --volumes-from "$upload_container:ro" alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b tar -C /data/uploads -czf - . > "$backup_path/uploads.tar.gz"
test -s "$backup_path/uploads.tar.gz"
docker compose images --format json > "$backup_path/images.json"
echo "Backup created: $backup_path. Copy it to encrypted off-host storage; verify restoration before deleting any older backup."
