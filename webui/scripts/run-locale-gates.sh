#!/bin/bash
# 用隔离的 Server、SQLite、MTS 与 Chromium 验证五种真实语言和伪语言。
# 用法：run-locale-gates.sh SERVER_BINARY OUTPUT_DIR
# 可选：BEAT_LOCALE_SMOKE_STATIC、BEAT_PLAYWRIGHT_MODULE、BEAT_BROWSER_EXECUTABLE。
set -Eeuo pipefail
umask 077

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
webui=$(dirname -- "$script_dir")
project=$(dirname -- "$webui")
server=${1:?usage: run-locale-gates.sh SERVER_BINARY OUTPUT_DIR}
output=${2:?usage: run-locale-gates.sh SERVER_BINARY OUTPUT_DIR}
static=${BEAT_LOCALE_SMOKE_STATIC:-"$webui/dist"}
playwright=${BEAT_PLAYWRIGHT_MODULE:-playwright}

die() { printf 'locale-gates: %s\n' "$*" >&2; exit 1; }
[[ -x "$server" ]] || die "server binary is not executable: $server"
[[ -f "$static/index.html" ]] || die "frontend build is missing: $static"

mkdir -p -- "$output"
chmod 0700 "$output"
work=$(mktemp -d "${TMPDIR:-/tmp}/beat-locale-gates.XXXXXX")
data="$work/data"
log="$output/server.log"
mkdir -m 0700 "$data"
: >"$log"
chmod 0600 "$log"
pid=

cleanup() {
    local status=$?
    trap - EXIT
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        kill -TERM "$pid" 2>/dev/null || true
        for _ in $(seq 1 40); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
        kill -KILL "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
    if ((status != 0)); then tail -80 "$log" >&2 || true; fi
    find "$work" -depth -delete
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

port=$(node -e 'const net=require("node:net");const s=net.createServer();s.unref();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close()})')
[[ "$port" =~ ^[0-9]+$ ]] || die "could not allocate a loopback port"
base="http://127.0.0.1:$port"
bootstrap_token=$(node -e 'process.stdout.write(require("node:crypto").randomBytes(24).toString("hex"))')
password=$(node -e 'process.stdout.write(require("node:crypto").randomBytes(24).toString("hex"))')

(
    cd "$project"
    BEAT_ADMIN_TOKEN="$bootstrap_token" BEAT_LOG_LEVEL=warn exec "$server" \
        -db-path "$data/beat.db" -mts-path "$data/mts" \
        -listen-addr "127.0.0.1:$port" -static-dir "$static"
) >"$log" 2>&1 &
pid=$!

ready=false
for _ in $(seq 1 120); do
    if curl -fsS "$base/readyz" >/dev/null 2>&1; then
        ready=true
        break
    fi
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.25
done
[[ "$ready" == true ]] || die "server did not become ready"

bootstrap_status=$(curl -sS -o "$work/bootstrap.json" -w '%{http_code}' -c "$work/cookies.txt" \
    -H "Origin: $base" -H 'Content-Type: application/json' \
    --data "{\"bootstrap_token\":\"$bootstrap_token\",\"username\":\"locale-owner\",\"display_name\":\"Locale Owner\",\"password\":\"$password\"}" \
    "$base/api/v1/auth/bootstrap")
[[ "$bootstrap_status" == 201 ]] || die "owner bootstrap returned $bootstrap_status"

# 创建节点是近期认证操作；不能把刚建立的会话误当成已经重新确认身份。
reauth_status=$(curl -sS -o "$work/reauth.json" -w '%{http_code}' -b "$work/cookies.txt" -c "$work/cookies.txt" \
    -H "Origin: $base" -H 'Content-Type: application/json' \
    --data "{\"password\":\"$password\"}" "$base/api/v1/auth/reauthenticate")
[[ "$reauth_status" == 200 ]] || die "recent authentication returned $reauth_status"

create_status=$(curl -sS -o "$work/node.json" -w '%{http_code}' -b "$work/cookies.txt" -c "$work/cookies.txt" \
    -H "Origin: $base" -H 'Content-Type: application/json' \
    --data "{\"name\":\"locale-node\",\"alias\":\"Locale Node\",\"host\":\"192.0.2.10\",\"port\":22,\"server_url\":\"$base\"}" \
    "$base/api/v1/nodes")
[[ "$create_status" == 201 ]] || die "public node creation returned $create_status"

(
    cd "$webui"
    BEAT_WEBUI_ROOT="$webui" \
    BEAT_SMOKE_USER=locale-owner \
    BEAT_SMOKE_PASSWORD="$password" \
    BEAT_SMOKE_ORIGIN="$base" \
    BEAT_PLAYWRIGHT_MODULE="$playwright" \
        node scripts/locale-pseudo-smoke.mjs "$base" "$output"
)
printf 'locale-gates: PASS (%s)\n' "$output"
