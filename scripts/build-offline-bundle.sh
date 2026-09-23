#!/usr/bin/env bash
# OPS-04: собирает автономный пакет для установки без сети.
#
#   scripts/build-offline-bundle.sh <версия> <каталог-пакета> <секретный-ключ-релиза>
#
# Пакет: образы (docker save), compose-файл, миграции, перечень файлов с
# контрольными суммами и подпись релиза Ed25519. Ключ подписи хранится вне
# сервера сборки установки; открытый ключ поставляется вместе с инстансом.
set -Eeuo pipefail
umask 022

version="${1:?версия релиза}"
bundle="${2:?каталог пакета}"
signing_key="${3:?файл секретного ключа релиза}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

[[ ! -e "$bundle" ]] || { echo "каталог пакета уже существует: $bundle" >&2; exit 1; }
mkdir -p "$bundle/images" "$bundle/migrations"
bundle="$(cd "$bundle" && pwd)"
signing_key="$(cd "$(dirname "$signing_key")" && pwd)/$(basename "$signing_key")"

export APP_VERSION="$version"
docker compose build
services=(backend nginx db)
image_args=()
for service in "${services[@]}"; do
  image="$(docker compose config --images | grep -E "[-_]${service}\$" | head -n 1)"
  [[ -n "$image" ]] || { echo "не найден образ сервиса $service" >&2; exit 1; }
  docker save "$image" -o "$bundle/images/$service.tar"
  digest="$(docker image inspect --format '{{.Id}}' "$image")"
  image_args+=("$service=$digest")
done

cp docker-compose.yml "$bundle/docker-compose.yml"
cp migrations/*.sql "$bundle/migrations/"
cp scripts/offline-install.sh "$bundle/offline-install.sh"

cd backend
tool="go run ./cmd/upgradecheck"
# Релиз описывает образы и миграции; перечень файлов пакета добавляется к нему
# и подписывается вместе с ним.
$tool release "$bundle/migrations" "$version" "${image_args[@]}" > "$bundle/release.json"
$tool files "$bundle" release.json release.signed > "$bundle/files.json"
python3 - "$bundle" <<'PY'
import json, sys
bundle = sys.argv[1]
release = json.load(open(f"{bundle}/release.json"))
release["files"] = json.load(open(f"{bundle}/files.json"))
json.dump(release, open(f"{bundle}/release.json", "w"), ensure_ascii=False, indent=2)
PY
rm "$bundle/files.json"
$tool sign "$bundle/release.json" "$signing_key" "$bundle/release.signed"
rm "$bundle/release.json"
echo "Пакет собран: $bundle (версия $version). Передайте его вместе с release.pub."
