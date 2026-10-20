#!/usr/bin/env bash
set -euo pipefail
umask 077

theme_repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$theme_repo_dir"

# BEAT_THEME_BIN 允许调用方注入已构建的 packer：前端/容器构建阶段不必重复下载 Go 模块。
theme_packer=${BEAT_THEME_BIN:-}
if [[ -z "$theme_packer" ]]; then
  theme_build_dir=$(mktemp -d "${TMPDIR:-/tmp}/beat-theme-build.XXXXXX")
  trap 'rm -rf -- "$theme_build_dir"' EXIT
  go build -trimpath -o "$theme_build_dir/beat-theme" ./cmd/theme
  theme_packer="$theme_build_dir/beat-theme"
fi
[[ -x "$theme_packer" ]] || { printf 'theme packer is missing or not executable: %s\n' "$theme_packer" >&2; exit 1; }

mkdir -p -m 0700 webui/public/themes
for theme_name in slate operations canvas graphite aurora terminal paper midnight solar pulse; do
  "$theme_packer" pack --source "themes/$theme_name" --output "webui/public/themes/$theme_name.theme.zip"
  "$theme_packer" validate --archive "webui/public/themes/$theme_name.theme.zip"
  install -m 0600 "themes/$theme_name/preview.png" "webui/public/themes/$theme_name.preview.png"
done
install -m 0600 docs/beat/theme-author-guide.md webui/public/themes/theme-author-guide.md
install -m 0600 docs/beat/theme-sandbox-contract.md webui/public/themes/theme-sandbox-contract.md
install -m 0600 docs/beat/theme-market-publishing.md webui/public/themes/theme-market-publishing.md
install -m 0600 themes/sdk/bridge.js webui/public/themes/beat-theme-sdk.js
install -m 0600 themes/LICENSE webui/public/themes/LICENSE
