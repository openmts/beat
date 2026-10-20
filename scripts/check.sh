#!/bin/bash
set -Eeuo pipefail
umask 077

project_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
check_dir=$(mktemp -d "${TMPDIR:-/tmp}/beat-check.XXXXXX")
check_container= check_image=

finish() {
    local status=$?
    trap - EXIT
    [[ -z "$check_container" ]] || docker rm --force "$check_container" >/dev/null || status=1
    [[ -z "$check_image" ]] || docker image rm "$check_image" >/dev/null || status=1
    rm -rf -- "$check_dir" || status=1
    exit "$status"
}
trap finish EXIT
trap 'exit 130' INT TERM HUP
cd "$project_dir"

require_tools() {
    local tool
    for tool in "$@"; do
        command -v "$tool" >/dev/null || { printf 'quality gate requires %s\n' "$tool" >&2; return 1; }
    done
}

check_backend() {
    require_tools go goimports-reviser golangci-lint govulncheck
    # 只比较规范化结果，不修改开发者已有的工作区差异。
    goimports-reviser -rm-unused -format -company-prefixes github.com/beat/backend \
        -set-exit-status -output stdout ./... >"$check_dir/formatted.txt"
    go mod tidy -diff
    go mod verify
    # race 插桩会把重包放大到 10 分钟以上（本机 internal/backup/notificationqueue/api 均超 400 s），
    # 因此显式限制包并发并放宽包超时：命令与宿主证据一致，避免把资源争用误报成回归。
    go test -race -shuffle=on -p 2 -timeout 20m ./...
    go test -coverprofile="$check_dir/coverage.out" ./...
    go tool cover -func="$check_dir/coverage.out" >"$check_dir/coverage.txt"
    awk '/^total:/ {gsub("%", "", $3); found=1; if ($3 + 0 < 90) exit 1} END {if (!found) exit 1}' "$check_dir/coverage.txt"
    golangci-lint run ./...
    go vet ./...
    govulncheck ./...
    go build -trimpath -o "$check_dir/beat-server" ./cmd/server
    go build -trimpath -o "$check_dir/beat-agent" ./cmd/agent
}

# check_frontend 也会重新生成主题分发资产（prebuild -> scripts/build-themes.sh），
# 因此需要 Go 工具链；构建后按后台示例主题列表逐一确认 ZIP、预览图、SDK 与下载版文档。
check_frontend() {
    require_tools npm go bash node
    (
        cd webui
        npm ci
        npm audit --registry=https://registry.npmjs.org --omit=dev --audit-level=high
        # 前端门禁固定单 worker：本机并行 worker 会在编辑器用例上争用 CPU 而超时（见 v13 批次记录）。
        npm test -- --maxWorkers=1
        npm run test:coverage -- --maxWorkers=1 --coverage.reportsDirectory="$check_dir/frontend-coverage"
        npm run lint
        npm run build -- --outDir "$check_dir/static"
        # B4：主题三套浏览器 smoke 进默认门禁。Playwright 来自本目录的 devDependency；
        # 浏览器装进本仓库专用目录，避免 install 顺手回收机器上其它 Playwright 版本的浏览器。
        # --only-shell 只装 headless shell（体积与系统依赖最小）。
        export PLAYWRIGHT_BROWSERS_PATH="${BEAT_PLAYWRIGHT_BROWSERS_PATH:-$HOME/.cache/beat-playwright}"
        node node_modules/playwright/cli.js install --only-shell chromium
        BEAT_THEME_BROWSER_STATIC="$check_dir/static" \
            bash scripts/run-theme-browser-gates.sh "$check_dir/theme-browser-gates"
    )
    # 后台示例主题列表是唯一真源：它列出的每个 id 都必须真的出现在构建产物里。
    local asset example_ids example_id
    example_ids=$(grep -o 'id: *"[^"]*"' webui/src/lib/theme-examples.ts | sed 's/.*"\([^"]*\)"/\1/')
    [[ -n "$example_ids" ]] || { printf 'no example themes parsed from webui/src/lib/theme-examples.ts\n' >&2; return 1; }
    while read -r example_id; do
        [[ -s "$check_dir/static/themes/$example_id.theme.zip" ]] || { printf 'missing theme archive: %s\n' "$example_id" >&2; return 1; }
        [[ -s "$check_dir/static/themes/$example_id.preview.png" ]] || { printf 'missing theme preview: %s\n' "$example_id" >&2; return 1; }
    done <<<"$example_ids"
    for asset in beat-theme-sdk.js theme-author-guide.md theme-sandbox-contract.md theme-market-publishing.md; do
        [[ -s "$check_dir/static/themes/$asset" ]] || { printf 'missing theme asset: %s\n' "$asset" >&2; return 1; }
    done
}

check_scripts() {
    require_tools bash python3 actionlint
    bash -n scripts/beatctl.sh scripts/check.sh scripts/prepare-release.sh
    PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/run.py
    actionlint
}

# check_themes 把关主题构建脚本与分发资产：cmd/theme 的交付测试会从 themes/ 源目录
# 重新 pack/validate，并在 webui/public/themes 存在时逐字节比对已发布产物。
check_themes() {
    require_tools go bash
    bash -n scripts/build-themes.sh
    go test ./cmd/theme/...
}

check_container() {
    require_tools docker
    check_image="beat-gate:$(basename "$check_dir" | tr '[:upper:]' '[:lower:]')"
    docker build --tag "$check_image" .
    check_container=$(docker run --detach --read-only --cap-drop=ALL --security-opt=no-new-privileges \
        --tmpfs /tmp:rw,noexec,nosuid,nodev,uid=65532,gid=65532,mode=0700 \
        --tmpfs /var/lib/beat:rw,nosuid,nodev,uid=65532,gid=65532,mode=0700 \
        --env BEAT_ADMIN_TOKEN=isolated-ci-bootstrap "$check_image")
    [[ $(docker exec "$check_container" id -u) == 65532 ]] || return 1
    local attempt
    for ((attempt=0; attempt<30; attempt++)); do
        if docker exec "$check_container" curl --fail --silent --show-error --max-time 2 http://127.0.0.1:8080/readyz >/dev/null; then return; fi
        sleep 1
    done
    docker logs "$check_container"
    return 1
}

[[ $# -le 1 ]] || { printf 'usage: check.sh [all|backend|frontend|scripts|themes|container]\n' >&2; exit 2; }
case ${1:-all} in
    backend) check_backend ;;
    frontend) check_frontend ;;
    scripts) check_scripts ;;
    themes) check_themes ;;
    container) check_container ;;
    all) check_scripts; check_themes; check_backend; check_frontend; check_container ;;
    *) printf 'unknown quality gate\n' >&2; exit 2 ;;
esac
