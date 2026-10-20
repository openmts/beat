#!/bin/sh
set -eu
umask 077

# 卷根保留生命周期锁，应用数据只放 data/；旧布局必须先冷备后显式迁移。
case "${1-}" in
    secrets) exec /usr/local/bin/beat-server "$@" ;;
esac
for legacy in beat.db beat.db-wal beat.db-shm beat_mts admin-data.key restore.pending.json backups themes .root-key-rotation .root-key-rotation.prepare .root-key-rotation.complete; do
    if [ -e "/var/lib/beat/$legacy" ] || [ -L "/var/lib/beat/$legacy" ]; then
        printf '%s\n' 'Beat found the previous container volume layout. Stop the old server, back up the complete volume, then move its application state into /var/lib/beat/data before restarting. See docs/beat/operations-runbook.md.' >&2
        exit 78
    fi
done
exec /usr/local/bin/beat-server "$@"
