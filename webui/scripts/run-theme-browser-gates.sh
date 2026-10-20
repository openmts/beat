#!/bin/bash
# 三套主题宿主浏览器门禁的一键运行器：每个 fixture 由 Go 用例起停（打印 FIXTURE=<url>），
# 读到 URL 后用 Playwright 跑对应的冒烟脚本，脚本结束时 POST /fixture/stop 让用例收尾。
#
# Playwright 由 webui 的 devDependency 提供（`npm ci` 即装好），浏览器缺省用 Playwright 自带的
# headless shell，所以宿主只需要能装包；仓库默认门禁经由 `scripts/check.sh frontend` 调用本脚本（B4）。
# 用法：
#   ./scripts/run-theme-browser-gates.sh [输出目录]
#
# 可选：BEAT_PLAYWRIGHT_MODULE（默认 playwright，按 webui/node_modules 解析；也可给 index.mjs 绝对路径）、
#      BEAT_BROWSER_EXECUTABLE（留空用 Playwright 自带浏览器）、
#      BEAT_WEBUI_ROOT（默认本脚本的上一级）、BEAT_THEME_BROWSER_STATIC（默认 <webui>/dist）、
#      GOCACHE、BEAT_THEME_GATE_TIMEOUT（默认 900 秒，作用在 fixture 等待与冒烟脚本上）。
set -Eeuo pipefail
umask 077

script_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
webui=${BEAT_WEBUI_ROOT:-$(dirname -- "$script_dir")}
project=$(dirname -- "$webui")
static=${BEAT_THEME_BROWSER_STATIC:-$webui/dist}
output=${1:-$webui/.theme-browser-gates}
timeout_seconds=${BEAT_THEME_GATE_TIMEOUT:-900}
playwright=${BEAT_PLAYWRIGHT_MODULE:-playwright}
chromium=${BEAT_BROWSER_EXECUTABLE:-}

die() { printf 'theme-browser-gates: %s\n' "$*" >&2; exit 1; }
[[ -f "$static/index.html" ]] || die "前端构建产物不存在：$static（先跑 npm run build）"

# run_gate <Go 用例> <冒烟脚本> <标记> <fixture 目录>
run_gate() {
    local test_name=$1 script=$2 marker=$3 fixture_dir=$4
    local shots="$output/$marker" log="$output/$marker.fixture.log"
    mkdir -p "$shots"
    : > "$log"
    ( cd "$project" && env GOCACHE="${GOCACHE:-/tmp/beat-go-cache}" \
        BEAT_THEME_BROWSER_FIXTURE=1 BEAT_DECLARATIVE_BROWSER_FIXTURE=1 BEAT_MARKET_BROWSER_FIXTURE=1 \
        BEAT_THEME_BROWSER_STATIC="$static" \
        go test ./internal/api/handler -run "^$test_name\$" -v -count=1 >"$log" 2>&1 ) &
    local test_pid=$! url=
    for _ in $(seq 1 "$timeout_seconds"); do
        url=$(grep -oE 'FIXTURE=http://[^[:space:]]+' "$log" | head -1 | cut -d= -f2- || true)
        [[ -n "$url" ]] && break
        kill -0 "$test_pid" 2>/dev/null || break
        sleep 1
    done
    [[ -n "$url" ]] || { printf 'FAIL %s：fixture 未就绪\n' "$marker"; tail -20 "$log"; return 1; }
    # 显式给了可执行文件才注入 BEAT_BROWSER_EXECUTABLE，否则让 Playwright 用自带浏览器。
    local browser_env=( BEAT_PLAYWRIGHT_MODULE="$playwright" )
    [[ -z "$chromium" ]] || browser_env+=( BEAT_BROWSER_EXECUTABLE="$chromium" )
    local args=( "$url" "$shots" )
    if [[ "$script" == theme-sandbox-smoke.mjs ]]; then
        args=( --server "$url" --output "$shots" --playwright "$playwright" )
        [[ -z "$chromium" ]] || args+=( --chromium "$chromium" )
    fi
    ( cd "$webui" && env "${browser_env[@]}" node "scripts/$script" "${args[@]}" )
    if wait "$test_pid"; then printf 'PASS %s（%s）\n' "$marker" "$shots"; else printf 'FAIL %s\n' "$marker"; tail -30 "$log"; return 1; fi
}

run_gate TestDeclarativeThemeBrowserFixture theme-declarative-smoke.mjs declarative declarative
run_gate TestThemeBrowserFixture theme-sandbox-smoke.mjs sandbox sandbox
run_gate TestThemeMarketBrowserFixture theme-market-smoke.mjs market market
printf 'theme-browser-gates: 三套门禁全部通过，截图与 fixture 日志在 %s\n' "$output"
