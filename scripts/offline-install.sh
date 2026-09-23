#!/usr/bin/env bash
# OPS-04: установка из автономного пакета без сети.
#
#   offline-install.sh <открытый-ключ-релиза>     # запускается из каталога пакета
#
# Порядок: подпись релиза и перечень файлов проверяются ДО загрузки образов;
# образы загружаются локально, их идентификаторы сверяются с подписанными;
# только затем поднимается стек. Любой сбой останавливает установку до запуска.
set -Eeuo pipefail

public_key="${1:?файл открытого ключа релиза}"
bundle="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$bundle"

command -v docker >/dev/null || { echo "нужен docker" >&2; exit 1; }
command -v upgradecheck >/dev/null || { echo "нужен upgradecheck в PATH (поставляется с инстансом)" >&2; exit 1; }
[[ -f release.signed ]] || { echo "нет release.signed" >&2; exit 1; }

echo "== подпись и состав пакета"
upgradecheck verify-tree "$bundle" release.signed "$public_key"

echo "== загрузка образов"
for archive in images/*.tar; do
  docker load -i "$archive"
done

echo "== запуск"
export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-cybercalculatorminc}"
docker compose -f docker-compose.yml up -d --no-build --pull never --wait --wait-timeout 180
echo "Установка завершена."
