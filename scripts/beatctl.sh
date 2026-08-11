#!/bin/sh
set -eu

prefix=${BEAT_PREFIX:-/opt/beat}
config_dir=${BEAT_CONFIG_DIR:-/etc/beat}
state_dir=${BEAT_STATE_DIR:-/var/lib/beat}
backup_dir=${BEAT_BACKUP_DIR:-/var/backups/beat}
service_name=${BEAT_SERVICE_NAME:-beat-server.service}
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
release_root=$(dirname "$script_dir")

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        echo "beatctl must run as root" >&2
        exit 1
    fi
}

require_release() {
    release_dir=$1
    test -x "$release_dir/beat-server"
    test -x "$release_dir/beat-agent"
    test -d "$release_dir/static"
}

install_release() {
    release_dir=$1
    version=$2
    destination="$prefix/releases/$version"
    install -d -m 0700 "$prefix/releases" "$destination" "$config_dir" "$state_dir" "$backup_dir"
    install -m 0700 "$release_dir/beat-server" "$destination/beat-server"
    install -m 0700 "$release_dir/beat-agent" "$destination/beat-agent"
    install -d -m 0700 "$destination/static"
    cp -R "$release_dir/static/." "$destination/static/"
    chmod -R go-rwx "$destination/static"
    ln -sfn "$destination" "$prefix/current"
}

ensure_config() {
    if [ ! -f "$config_dir/beat.env" ]; then
        install -m 0600 "$release_root/deploy/beat.env.example" "$config_dir/beat.env"
        echo "edit $config_dir/beat.env before starting Beat" >&2
    fi
    install -m 0600 "$release_root/deploy/systemd/beat-server.service" /etc/systemd/system/beat-server.service
    systemctl daemon-reload
}

backup_state() {
    timestamp=$(date -u +%Y%m%dT%H%M%SZ)
    archive="$backup_dir/beat-state-$timestamp.tar.gz"
    install -d -m 0700 "$backup_dir"
    tar -C "$state_dir" -czf "$archive" .
    chmod 0600 "$archive"
    echo "$archive"
}

install_command() {
    release_dir=$1
    version=$2
    require_release "$release_dir"
    install_release "$release_dir" "$version"
    ensure_config
    systemctl enable --now "$service_name"
}

upgrade_command() {
    release_dir=$1
    version=$2
    require_release "$release_dir"
    systemctl stop "$service_name"
    archive=$(backup_state)
    install_release "$release_dir" "$version"
    systemctl start "$service_name"
    echo "backup: $archive"
}

rollback_command() {
    version=$1
    archive=$2
    target="$prefix/releases/$version"
    test -x "$target/beat-server"
    test -f "$archive"
    systemctl stop "$service_name"
    current_backup=$(backup_state)
    timestamp=$(date -u +%Y%m%dT%H%M%SZ)
    mv "$state_dir" "$state_dir.pre-rollback-$timestamp"
    install -d -m 0700 "$state_dir"
    tar -C "$state_dir" -xzf "$archive"
    ln -sfn "$target" "$prefix/current"
    systemctl start "$service_name"
    echo "pre-rollback backup: $current_backup"
}

uninstall_command() {
    systemctl disable --now "$service_name" || true
    rm -f /etc/systemd/system/beat-server.service
    systemctl daemon-reload
    echo "binaries, configuration, state, and backups were retained under $prefix, $config_dir, $state_dir, and $backup_dir"
}

usage() {
    echo "usage: beatctl.sh install <release-dir> <version>" >&2
    echo "       beatctl.sh upgrade <release-dir> <version>" >&2
    echo "       beatctl.sh rollback <version> <state-backup.tar.gz>" >&2
    echo "       beatctl.sh uninstall" >&2
    exit 2
}

require_root
case ${1:-} in
    install) [ "$#" -eq 3 ] || usage; install_command "$2" "$3" ;;
    upgrade) [ "$#" -eq 3 ] || usage; upgrade_command "$2" "$3" ;;
    rollback) [ "$#" -eq 3 ] || usage; rollback_command "$2" "$3" ;;
    uninstall) [ "$#" -eq 1 ] || usage; uninstall_command ;;
    *) usage ;;
esac
