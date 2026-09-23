#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
source_dir="$repo_root/frontend"
target_dir="$repo_root/backend/internal/modules/webapp/dist"

mkdir -p "$target_dir/components"
cp "$source_dir/index.html" "$source_dir/app.js" "$source_dir/workspace.js" \
  "$source_dir/mfa.js" "$source_dir/features.js" "$source_dir/screens.js" \
  "$source_dir/okz.js" "$source_dir/style.css" "$source_dir/theme.css" "$target_dir/"
rm -rf "$target_dir/core" "$target_dir/shell" "$target_dir/screens"
cp -R "$source_dir/core" "$source_dir/shell" "$source_dir/screens" "$target_dir/"
cp "$source_dir/components/ui.js" "$target_dir/components/ui.js"
