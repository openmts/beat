#!/usr/bin/env bash
set -euo pipefail
umask 077

theme_repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$theme_repo_dir"

# BEAT_THEME_BIN 允许调用方复用已构建的 packer；未提供时在临时目录构建并清理。
theme_packer=${BEAT_THEME_BIN:-}
theme_build_dir=""
if [[ -z "$theme_packer" ]]; then
  theme_build_dir=$(mktemp -d "${TMPDIR:-/tmp}/beat-theme-build.XXXXXX")
  go build -trimpath -o "$theme_build_dir/beat-theme" ./cmd/theme
  theme_packer="$theme_build_dir/beat-theme"
fi
[[ -x "$theme_packer" ]] || { printf 'theme packer is missing or not executable: %s\n' "$theme_packer" >&2; exit 1; }

install -d -m 0700 webui/public
themes_dir=webui/public/themes
staging_dir=webui/public/.themes.$$
cleanup() {
  rm -rf -- "$staging_dir"
  if [[ -n "$theme_build_dir" ]]; then rm -rf -- "$theme_build_dir"; fi
}
trap cleanup EXIT
mkdir -m 0700 "$staging_dir"

for theme_name in default paper; do
  "$theme_packer" pack --source "themes/$theme_name" --output "$staging_dir/$theme_name.theme.zip"
  "$theme_packer" validate --archive "$staging_dir/$theme_name.theme.zip"
  install -m 0600 "themes/$theme_name/preview.png" "$staging_dir/$theme_name.preview.png"
done
install -m 0600 docs/beat/theme-author-guide.md "$staging_dir/theme-author-guide.md"
install -m 0600 docs/beat/theme-sandbox-contract.md "$staging_dir/theme-sandbox-contract.md"
install -m 0600 docs/beat/theme-market-publishing.md "$staging_dir/theme-market-publishing.md"
install -m 0600 themes/sdk/bridge.js "$staging_dir/beat-theme-sdk.js"
install -m 0600 themes/LICENSE "$staging_dir/LICENSE"

# 原子替换分发目录，避免旧版本留下的 ZIP 与预览图混入新构建。
rm -rf -- "$themes_dir"
mv -- "$staging_dir" "$themes_dir"
trap - EXIT
if [[ -n "$theme_build_dir" ]]; then rm -rf -- "$theme_build_dir"; fi
