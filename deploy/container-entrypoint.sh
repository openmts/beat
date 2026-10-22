#!/bin/sh
set -eu
umask 077

role=${BEAT_CONTAINER_ROLE:-server}
case "${1-}" in
    server|agent)
        role=$1
        shift
        ;;
    secrets)
        exec /usr/local/bin/beat-server "$@"
        ;;
esac

run_server() {
    # 卷根保留生命周期锁，应用数据只放 data/；旧布局必须先冷备后显式迁移。
    for legacy in beat.db beat.db-wal beat.db-shm beat_mts admin-data.key restore.pending.json backups themes .root-key-rotation .root-key-rotation.prepare .root-key-rotation.complete; do
        if [ -e "/var/lib/beat/$legacy" ] || [ -L "/var/lib/beat/$legacy" ]; then
            printf '%s\n' 'Beat found the previous container volume layout. Stop the old server, back up the complete volume, then move its application state into /var/lib/beat/data before restarting. See docs/beat/operations-runbook.md.' >&2
            exit 78
        fi
    done
    exec /usr/local/bin/beat-server "$@"
}

case "$role" in
    server) run_server "$@" ;;
    agent) exec /usr/local/bin/beat-agent "$@" ;;
    *)
        printf 'unsupported BEAT_CONTAINER_ROLE: %s\n' "$role" >&2
        exit 64
        ;;
esac
