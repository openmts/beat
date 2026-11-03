#!/bin/bash
# 用真实 Server、SQLite、MTS 与 Chromium 验证首次初始化、登录和后台配置持久化。
# 用法：run-app-browser-gates.sh SERVER_BINARY OUTPUT_DIR
# 可选：BEAT_APP_BROWSER_AGENT、BEAT_APP_BROWSER_SSH、BEAT_APP_BROWSER_STATIC、
#       BEAT_PLAYWRIGHT_MODULE、BEAT_BROWSER_EXECUTABLE。
set -Eeuo pipefail
umask 077

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
webui=$(dirname -- "$script_dir")
project=$(dirname -- "$webui")
server=${1:?usage: run-app-browser-gates.sh SERVER_BINARY OUTPUT_DIR}
output=${2:?usage: run-app-browser-gates.sh SERVER_BINARY OUTPUT_DIR}
static=${BEAT_APP_BROWSER_STATIC:-"$webui/dist"}
playwright=${BEAT_PLAYWRIGHT_MODULE:-playwright}
agent=${BEAT_APP_BROWSER_AGENT:-}
with_ssh=${BEAT_APP_BROWSER_SSH:-0}

die() { printf 'app-browser-gates: %s\n' "$*" >&2; exit 1; }
[[ -x "$server" ]] || die "server binary is not executable: $server"
[[ -z "$agent" || -x "$agent" ]] || die "agent binary is not executable: $agent"
[[ "$with_ssh" != 1 || -n "$agent" ]] || die "agent binary is required for the terminal smoke"
[[ -f "$static/index.html" ]] || die "frontend build is missing: $static"

mkdir -p -- "$output"
chmod 0700 "$output"
work=$(mktemp -d "${TMPDIR:-/tmp}/beat-app-browser.XXXXXX")
data="$work/data"
mkdir -m 0700 "$data"
log="$output/server.log"
: >"$log"
chmod 0600 "$log"
pid=
sshd_pid=
ssh_port=
ssh_authorized_keys=

allocate_loopback_port() {
    node -e 'const net=require("node:net");const s=net.createServer();s.unref();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close()})'
}

wait_for_port() {
    node -e 'const net=require("node:net");const [host,port]=process.argv.slice(1);const s=net.createConnection({host,port:Number(port)});s.once("connect",()=>{s.destroy();process.exit(0)});s.once("error",()=>process.exit(1));setTimeout(()=>process.exit(1),500)' "$1" "$2"
}

start_ssh_fixture() {
    local sshdir="$work/sshd" sshd_log="$output/sshd.log"
    mkdir -m 0700 "$sshdir"
    : >"$sshdir/authorized_keys"
    chmod 0600 "$sshdir/authorized_keys"
    ssh-keygen -q -t ed25519 -N '' -f "$sshdir/host_key" >/dev/null
    ssh_port=$(allocate_loopback_port)
    [[ "$ssh_port" =~ ^[0-9]+$ ]] || die "could not allocate an sshd port"
    cat >"$sshdir/sshd_config" <<EOF
ListenAddress 127.0.0.1:$ssh_port
HostKey $sshdir/host_key
PidFile $sshdir/sshd.pid
AuthorizedKeysFile $sshdir/authorized_keys
PermitRootLogin prohibit-password
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
AllowUsers root
StrictModes no
PrintMotd no
UseDNS no
PerSourcePenalties no
LogLevel VERBOSE
EOF
    chmod 0600 "$sshdir/sshd_config"
    /usr/bin/sshd -t -f "$sshdir/sshd_config"
    : >"$sshd_log"
    chmod 0600 "$sshd_log"
    /usr/bin/sshd -D -e -f "$sshdir/sshd_config" >>"$sshd_log" 2>&1 &
    sshd_pid=$!
    for _ in $(seq 1 40); do
        wait_for_port 127.0.0.1 "$ssh_port" && break
        kill -0 "$sshd_pid" 2>/dev/null || die "sshd exited during startup"
        sleep 0.1
    done
    wait_for_port 127.0.0.1 "$ssh_port" || die "sshd did not start"
    ssh_host=127.0.0.1
    ssh_authorized_keys="$sshdir/authorized_keys"
}

cleanup() {
    local status=$?
    trap - EXIT
    if [[ -n "$sshd_pid" ]] && kill -0 "$sshd_pid" 2>/dev/null; then
        kill -TERM "$sshd_pid" 2>/dev/null || true
        for _ in $(seq 1 40); do kill -0 "$sshd_pid" 2>/dev/null || break; sleep 0.1; done
        kill -KILL "$sshd_pid" 2>/dev/null || true
        wait "$sshd_pid" 2>/dev/null || true
    fi
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        kill -TERM "$pid" 2>/dev/null || true
        for _ in $(seq 1 40); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
        kill -KILL "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
    fi
    if ((status != 0)); then tail -40 "$log" >&2 || true; fi
    if ((status != 0)) && [[ -s "$output/sshd.log" ]]; then tail -40 "$output/sshd.log" >&2 || true; fi
    find "$work" -depth -delete
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT TERM HUP

port=$(allocate_loopback_port)
[[ "$port" =~ ^[0-9]+$ ]] || die "could not allocate a loopback port"
base="http://127.0.0.1:$port"
bootstrap_token=$(node -e 'process.stdout.write(require("node:crypto").randomBytes(24).toString("hex"))')
password=$(node -e 'process.stdout.write(require("node:crypto").randomBytes(24).toString("hex"))')

if [[ "$with_ssh" == 1 ]]; then
    [[ "$EUID" -eq 0 ]] || die "terminal smoke must run as root because Beat connects as root"
    [[ -x /usr/bin/sshd ]] || die "sshd is required for the terminal smoke"
    command -v ssh-keygen >/dev/null || die "ssh-keygen is required for the terminal smoke"
    start_ssh_fixture
fi

(
    cd "$project"
    BEAT_ADMIN_TOKEN="$bootstrap_token" BEAT_LOG_LEVEL=warn exec "$server" \
        -db-path "$data/beat.db" -mts-path "$data/mts" \
        -listen-addr "127.0.0.1:$port" -static-dir "$static"
) >"$log" 2>&1 &
pid=$!

ready=false
for _ in $(seq 1 120); do
    if node -e 'fetch(process.argv[1]+"/readyz").then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))' "$base"; then
        ready=true
        break
    fi
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.25
done
[[ "$ready" == true ]] || die "server did not become ready"

(
    cd "$webui"
    BEAT_PLAYWRIGHT_MODULE="$playwright" \
    BEAT_APP_BROWSER_AGENT="$agent" \
    BEAT_E2E_SSH_HOST="${ssh_host:-}" \
    BEAT_E2E_SSH_PORT="${ssh_port:-}" \
    BEAT_E2E_SSH_AUTHORIZED_KEYS="${ssh_authorized_keys:-}" \
    BEAT_E2E_BOOTSTRAP_TOKEN="$bootstrap_token" \
    BEAT_E2E_USERNAME="browser-owner" \
    BEAT_E2E_PASSWORD="$password" \
        node scripts/app-browser-smoke.mjs "$base" "$output"
)
printf 'app-browser-gates: PASS (%s)\n' "$output"
