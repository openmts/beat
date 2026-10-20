#!/bin/bash
set -Eeuo pipefail
umask 077

prefix=${BEAT_PREFIX:-/opt/beat}
config_dir=${BEAT_CONFIG_DIR:-/etc/beat}
state_dir=${BEAT_STATE_DIR:-/var/lib/beat}
backup_dir=${BEAT_BACKUP_DIR:-/var/backups/beat}
unit_dir=${BEAT_UNIT_DIR:-/etc/systemd/system}
service_name=${BEAT_SERVICE_NAME:-beat-server.service}
# A9 参数化：监听地址与运行账户可由环境覆盖，缺省值与既有安装完全一致。
listen_addr=${BEAT_LISTEN_ADDR:-:8080}
service_account=${BEAT_SERVICE_ACCOUNT:-beat}
ready_attempts=${BEAT_READY_ATTEMPTS:-30}
script_dir=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
workspace= stage= candidate_data= before_data= checkpoint= old_pointer=
stopped=0 was_active=0 had_unit=0 data_changed=0 unit_changed=0

die() { printf 'beatctl: %s\n' "$*" >&2; exit 1; }

# 单元文件尚未安装（首次安装）时，systemctl stop/disable 会以“not loaded”失败；这类调用必须先判断。
unit_present() { [[ -e "$unit_dir/$service_name" ]] || systemctl is-active --quiet "$service_name"; }

usage() {
    printf '%s\n' 'Requires Linux, systemd, Bash and Python 3 (standard library).' \
        'usage: beatctl.sh install|upgrade <release-dir> <version>' \
        '       beatctl.sh rollback <version> <state-backup.tar.gz>' \
        '       beatctl.sh uninstall'         '' \
        'Environment (all optional, defaults preserve the previous deployment):' \
        '  BEAT_PREFIX=/opt/beat        release root' \
        '  BEAT_CONFIG_DIR=/etc/beat    private configuration' \
        '  BEAT_STATE_DIR=/var/lib/beat database, metric store and account home' \
        '  BEAT_LISTEN_ADDR=:8080       server listen address' \
        '  BEAT_SERVICE_ACCOUNT=beat    dedicated unprivileged account' >&2
    exit 2
}

validate_paths() {
    local path ancestor other
    local -A seen=()
    [[ "$service_name" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.@-]*\.service$ ]] || die 'invalid service name'
    [[ "$ready_attempts" =~ ^[1-9][0-9]?$ ]] || die 'ready attempts must be between 1 and 99'
    validate_account_name
    validate_listen_addr
    for path in "$prefix" "$config_dir" "$state_dir" "$backup_dir" "$unit_dir"; do
        [[ "$path" =~ ^/[a-zA-Z0-9_./-]+$ && "$path" != / && "$path" == "$(realpath -m -- "$path")" ]] || die 'paths must be clean absolute paths without spaces'
        [[ -z ${seen[$path]+present} ]] || die 'deployment directories must be distinct'
        seen["$path"]=1
        ancestor=$path
        while [[ "$ancestor" != / ]]; do
            [[ ! -L "$ancestor" && ( ! -e "$ancestor" || -d "$ancestor" ) ]] || die "unsafe directory: $ancestor"
            ancestor=$(dirname -- "$ancestor")
        done
        for other in "$prefix" "$config_dir" "$state_dir" "$backup_dir" "$unit_dir"; do
            [[ "$path" != "$other/"* ]] || die 'deployment directories must not contain one another'
        done
    done
}

ensure_directory() {
    [[ -d "$1" ]] && return 0
    ensure_directory "$(dirname -- "$1")"
    install -d -m 0700 -- "$1"
}

# 账户名要能安全地写进单元文件与 sed 表达式，且不允许借用 root/系统账号。
validate_account_name() {
    [[ "$service_account" =~ ^[a-z_][a-z0-9_-]{0,30}$ ]] || die 'service account must be a lowercase system account name'
    case "$service_account" in root|daemon|bin|sys|nobody|systemd-*) die 'service account must be a dedicated unprivileged account' ;; esac
}

# 监听地址只接受 host:port；非法字符会破坏 sed 与单元文件语法，所以在这里一次性拦掉。
validate_listen_addr() {
    local port
    [[ "$listen_addr" =~ ^(\[[0-9a-fA-F:]+\]|[0-9a-zA-Z._-]*|\*):[0-9]{1,5}$ ]] \
        || die 'listen address must be host:port without spaces or shell metacharacters'
    port=${listen_addr##*:}
    (( 10#$port >= 1 && 10#$port <= 65535 )) || die 'listen port must be between 1 and 65535'
}

# 就绪探针使用可路由的回环地址：通配与空主机（:8080、0.0.0.0:8080、[::]:8080）都落到 127.0.0.1。
ready_url() {
    local host=${listen_addr%:*} port=${listen_addr##*:}
    host=${host#[}
    host=${host%]}
    case "$host" in ''|0.0.0.0|'::'|'*') host=127.0.0.1 ;; esac
    printf 'http://%s:%s/readyz\n' "$host" "$port"
}

# 现有单元的管理性判定：ExecStart 与 User 必须与本次参数完全一致。
unit_exec_start() {
    printf 'ExecStart=%s/current/beat-server -db-path %s/beat.db -mts-path %s/beat_mts -static-dir %s/current/static -listen-addr %s\n' \
        "$prefix" "$state_dir" "$state_dir" "$prefix" "$listen_addr"
}

validate_account() {
    local entry account_name account_password account_uid account_gid account_comment account_home account_shell unit owner processes lookup group_entry group_name group_password group_gid members
    if entry=$(getent passwd "$service_account"); then :; else
        lookup=$?
        [[ "$lookup" == 2 ]] || die "cannot inspect $service_account account"
        useradd --system --user-group --no-create-home --home-dir "$state_dir" --shell /usr/sbin/nologin --comment 'Beat monitoring service' "$service_account"
        entry=$(getent passwd "$service_account")
    fi
    IFS=: read -r account_name account_password account_uid account_gid account_comment account_home account_shell <<<"$entry"
    [[ "$account_name" == "$service_account" && "$account_uid" =~ ^[1-9][0-9]*$ && "$account_gid" =~ ^[1-9][0-9]*$ ]] || die "$service_account must be an unprivileged dedicated account"
    [[ "$account_home" == "$state_dir" && "$account_comment" == 'Beat monitoring service' ]] || die "existing $service_account account is not owned by this installer"
    case "$account_shell" in /usr/sbin/nologin|/sbin/nologin|/usr/bin/false|/bin/false) ;; *) die "$service_account must use a non-login shell" ;; esac
    group_entry=$(getent group "$service_account")
    IFS=: read -r group_name group_password group_gid members <<<"$group_entry"
    [[ "$group_name" == "$service_account" && "$group_gid" == "$account_gid" && $(id -nG "$service_account") == "$service_account" ]] || die "$service_account must use only its private unprivileged group"
    processes=$(ps -u "$account_uid" -o unit=) || [[ $? == 1 ]] || die "cannot inspect $service_account account processes"
    for unit in $processes; do [[ "$unit" == "$service_name" ]] || die "$service_account account is used by another process"; done
    systemctl list-unit-files --type=service --no-legend --no-pager >"$workspace/units"
    while read -r unit _; do
        [[ -n "$unit" && "$unit" != "$service_name" ]] || continue
        # 模板单元（`foo@.service`）没有实例，systemctl show 会直接失败；真正跑起来的模板实例
        # 由上面的进程检查兜住，这里只跳过模板文件本身，避免在标准主机上中断安装。
        [[ "$unit" != *"@.service" ]] || continue
        # 已屏蔽/未加载的单元同样可能查不到属性：与本安装器无关，不作为冲突。
        owner=$(systemctl show "$unit" --property=User --value 2>/dev/null) || continue
        [[ "$owner" != "$service_account" ]] || die "$service_account account is used by $unit"
    done <"$workspace/units"
}

initialize() {
    [[ $(id -u) == 0 ]] || die 'run beatctl as root'
    local tool
    for tool in python3 systemctl useradd getent ps install tar curl flock realpath; do command -v "$tool" >/dev/null || die "required tool is missing: $tool"; done
    [[ -f "$script_dir/state_archive.py" && ! -L "$script_dir/state_archive.py" ]] || die 'state_archive.py must accompany beatctl.sh'
    validate_paths
    ensure_directory "$config_dir"
    [[ ! -L "$config_dir/deployment.lock" ]] || die 'unsafe deployment lock'
    exec 9>"$config_dir/deployment.lock"
    flock -n 9 || die 'another deployment is running'
    workspace=$(mktemp -d "$config_dir/.beatctl.XXXXXX")
    ensure_directory "$backup_dir"
    chown root:root "$config_dir" "$backup_dir"
    chmod 0700 "$config_dir" "$backup_dir"
    ensure_directory "$unit_dir"
    [[ ! -L "$unit_dir/$service_name" && ( ! -e "$unit_dir/$service_name" || -f "$unit_dir/$service_name" ) ]] || die 'unsafe service unit'
    [[ ! -L "$prefix/releases" && ( ! -e "$prefix/releases" || -d "$prefix/releases" ) ]] || die 'unsafe releases directory'
    if [[ -f "$unit_dir/$service_name" ]]; then
        grep -Fxq "$(unit_exec_start)" "$unit_dir/$service_name" || die 'existing unit is not managed by this installer'
        grep -Fxq "User=$service_account" "$unit_dir/$service_name" || die 'existing unit is not managed by this installer'
    fi
    if [[ -e "$prefix/current" || -L "$prefix/current" ]]; then
        [[ -L "$prefix/current" ]] || die 'current must be a managed symbolic link'
        old_pointer=$(readlink -f -- "$prefix/current")
        [[ "$old_pointer" == "$prefix/releases/"* && -d "$old_pointer" ]] || die 'current points outside managed releases'
    fi
    [[ "$action" == uninstall ]] || validate_account
}

require_release() {
    local binary
    [[ -d "$1/static" && -f "$1/static/index.html" && -f "$1/deploy/systemd/beat-server.service" ]] || die 'release is missing static assets or its service template'
    [[ -z $(find "$1" ! -type d ! -type f -print -quit) ]] || die 'release contains links or special files'
    for binary in beat-server beat-agent; do
        [[ -f "$1/$binary" && -x "$1/$binary" ]] || die "release is missing $binary"
        "$1/$binary" --version >/dev/null
    done
}

prepare_release() {
    local source=$1 version=$2
    [[ "$version" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ && "$version" != *..* ]] || die 'invalid release version'
    require_release "$source"
    ensure_directory "$prefix/releases"
    selected="$prefix/releases/$version"
    if [[ -e "$selected" ]]; then
        require_release "$selected"
        cmp "$source/beat-server" "$selected/beat-server"
        cmp "$source/beat-agent" "$selected/beat-agent"
        diff -qr "$source/static" "$selected/static" >/dev/null
        return 0
    fi
    stage=$(mktemp -d "$prefix/releases/.stage.XXXXXX")
    install -m 0700 "$source/beat-server" "$stage/beat-server"
    install -m 0700 "$source/beat-agent" "$stage/beat-agent"
    install -d -m 0700 "$stage/static" "$stage/deploy/systemd"
    cp -R -- "$source/static/." "$stage/static/"
    find "$stage/static" -type f -exec chmod 0600 {} +
    find "$stage/static" -type d -exec chmod 0700 {} +
    install -m 0600 "$source/deploy/systemd/beat-server.service" "$stage/deploy/systemd/beat-server.service"
    chown -R "$service_account:$service_account" "$stage"
    mv -T -- "$stage" "$selected"
    stage=
}

prepare_unit() {
    local lifecycle_lock
    ensure_directory "$(dirname -- "$state_dir")"
    lifecycle_lock=$(python3 -c 'import hashlib,pathlib,sys; p=pathlib.Path(sys.argv[1]).resolve(); print(p.parent / (".beat-lifecycle-"+hashlib.sha256(str(p).encode()).hexdigest()+".lock"))' "$state_dir")
    python3 -c 'import os,stat,sys; p,account=sys.argv[1:]; f=os.open(p,os.O_RDWR|os.O_CREAT|os.O_NOFOLLOW|os.O_NONBLOCK,0o600); s=os.fstat(f); fields=account.split(":"); valid=stat.S_ISREG(s.st_mode) and stat.S_IMODE(s.st_mode)==0o600 and s.st_nlink==1 and s.st_uid in (0,int(fields[2])); valid or sys.exit("unsafe lifecycle lock"); os.fchown(f,int(fields[2]),int(fields[3])); os.fsync(f); os.close(f)' "$lifecycle_lock" "$(getent passwd "$service_account")"
    [[ ! -L "$config_dir/beat.env" ]] || die 'unsafe environment file'
    if [[ ! -f "$config_dir/beat.env" ]]; then
        python3 -c 'import secrets; print("BEAT_ADMIN_TOKEN="+secrets.token_hex(32)); print("BEAT_AGENT_TOKEN=\nBEAT_TRUSTED_PROXIES=\nBEAT_LOG_LEVEL=info")' >"$config_dir/beat.env"
        printf 'bootstrap credential saved privately in %s/beat.env\n' "$config_dir"
    fi
    chmod 0600 "$config_dir/beat.env"
    chown root:root "$config_dir/beat.env"
    sed -e "s|/opt/beat|$prefix|g" -e "s|/etc/beat|$config_dir|g" -e "s|/var/lib/beat|$state_dir|g" -e "s|/var/lib/.beat-lifecycle-[0-9a-f]*\.lock|$lifecycle_lock|g" \
        -e "s|^User=beat$|User=$service_account|" -e "s|^Group=beat$|Group=$service_account|" \
        -e "s| -listen-addr :8080$| -listen-addr $listen_addr|" \
        "$selected/deploy/systemd/beat-server.service" >"$workspace/new-unit"
    [[ $(awk '/^User=/{print $0}' "$workspace/new-unit") == "User=$service_account" ]] || die "release must run under the dedicated $service_account account"
    grep -Fq -- " -listen-addr $listen_addr" "$workspace/new-unit" || die 'generated unit does not carry the requested listen address'
    local setting
    for setting in "Group=$service_account" 'ProtectSystem=strict' "ReadOnlyPaths=$prefix" "ReadWritePaths=$state_dir" "ReadWritePaths=$lifecycle_lock" 'NoNewPrivileges=yes'; do
        grep -Fxq "$setting" "$workspace/new-unit" || die 'release service template lacks required isolation'
    done
}

prepare_restore() {
    ensure_directory "$(dirname -- "$state_dir")"
    candidate_data=$(mktemp -d "$state_dir.restore.XXXXXX")
    python3 "$script_dir/state_archive.py" "$1" "$candidate_data"
}

publish_pointer() {
    local temporary
    temporary=$(mktemp "$prefix/.current.XXXXXX") || return 1
    rm -f -- "$temporary" || return 1
    ln -s -- "$1" "$temporary" || return 1
    if mv -Tf -- "$temporary" "$prefix/current"; then return 0; fi
    rm -f -- "$temporary" || return 1
    return 1
}

wait_ready() {
    local attempt
    for ((attempt=0; attempt<ready_attempts; attempt++)); do
        if systemctl is-active --quiet "$service_name" && curl --fail --silent --show-error --max-time 2 "$(ready_url)" >/dev/null; then return 0; fi
        sleep 1
    done
    return 1
}

checkpoint_state() {
    checkpoint=$(mktemp -d "$backup_dir/beat-state-$(date -u +%Y%m%dT%H%M%SZ).XXXXXX")
    printf '%s\n' "$old_pointer" >"$checkpoint/previous-target"
    if [[ -f "$unit_dir/$service_name" ]]; then
        install -m 0600 "$unit_dir/$service_name" "$checkpoint/previous.service"
        had_unit=1
    fi
    if [[ -d "$state_dir" ]]; then
        tar -C "$state_dir" -czf "$checkpoint/state.tar.gz" .
        if [[ -z "$candidate_data" ]]; then prepare_restore "$checkpoint/state.tar.gz"; fi
    elif [[ -z "$candidate_data" ]]; then
        ensure_directory "$(dirname -- "$state_dir")"
        candidate_data=$(mktemp -d "$state_dir.restore.XXXXXX")
    fi
}

activate() {
    if systemctl is-active --quiet "$service_name"; then was_active=1; fi
    stopped=1
    if unit_present; then systemctl stop "$service_name"; fi
    checkpoint_state
    if [[ -d "$state_dir" ]]; then
        before_data=$(mktemp -d "$state_dir.previous.XXXXXX")
        mv -T -- "$state_dir" "$before_data"
        data_changed=1
        printf '%s\n' "$before_data" >"$checkpoint/previous-data"
    fi
    data_changed=1
    mv -T -- "$candidate_data" "$state_dir"
    candidate_data=
    chown -R "$service_account:$service_account" "$state_dir"
    chown "$service_account:$service_account" "$prefix" "$prefix/releases"
    chown -R beat:beat "$selected"
    chmod 0700 "$prefix" "$prefix/releases" "$state_dir"
    unit_changed=1
    install -m 0600 "$workspace/new-unit" "$unit_dir/$service_name"
    publish_pointer "$selected"
    systemctl daemon-reload
    systemctl enable "$service_name"
    systemctl start "$service_name"
    wait_ready
    stopped=0
    if [[ -n "$before_data" ]]; then
        if rm -rf -- "$before_data"; then
            before_data=
            rm -f -- "$checkpoint/previous-data" || printf 'ready; stale checkpoint metadata retained in %s\n' "$checkpoint" >&2
        else
            printf 'ready; previous data retained for manual cleanup: %s\n' "$before_data" >&2
        fi
    fi
    printf 'ready: %s; recovery checkpoint: %s\n' "$selected" "$checkpoint"
}

recover_previous() {
    local failed_data
    if unit_present; then systemctl stop "$service_name" || return 1; fi
    if ((data_changed)); then
        if [[ -e "$state_dir" ]]; then
            failed_data=$(mktemp -d "$state_dir.failed.XXXXXX") || return 1
            mv -T -- "$state_dir" "$failed_data" || return 1
            chown root:root "$failed_data" || return 1
            printf '%s\n' "$failed_data" >"$checkpoint/failed-data-path" || return 1
        fi
        if [[ -n "$before_data" ]]; then mv -T -- "$before_data" "$state_dir" || return 1; before_data=; fi
    fi
    if [[ -n "$old_pointer" ]]; then publish_pointer "$old_pointer" || return 1; else rm -f -- "$prefix/current" || return 1; fi
    if ((unit_changed)); then
        if ((had_unit)); then install -m 0600 "$checkpoint/previous.service" "$unit_dir/$service_name" || return 1; else systemctl disable "$service_name" && rm -f -- "$unit_dir/$service_name" || return 1; fi
    fi
    systemctl daemon-reload || return 1
    if ((was_active)); then systemctl start "$service_name" && wait_ready || return 1; fi
    printf 'restored previous release and data; checkpoint: %s\n' "$checkpoint" >&2
}

finish() {
    local status=$?
    trap - EXIT
    if ((status != 0 && stopped)); then
        recover_previous || printf 'automatic recovery incomplete; preserve checkpoint %s and data %s\n' "$checkpoint" "$before_data" >&2
    fi
    [[ -z "$workspace" ]] || rm -rf -- "$workspace"
    [[ -z "$stage" ]] || rm -rf -- "$stage"
    [[ -z "$candidate_data" ]] || rm -rf -- "$candidate_data"
    exit "$status"
}

trap finish EXIT
trap 'exit 130' INT TERM HUP
[[ $# -gt 0 ]] || usage
action=$1
case "$action" in install|upgrade|rollback) [[ $# == 3 ]] || usage ;; uninstall) [[ $# == 1 ]] || usage ;; *) usage ;; esac
initialize
if [[ "$action" == uninstall ]]; then
    if unit_present; then systemctl disable --now "$service_name"; fi
    rm -f -- "$unit_dir/$service_name"
    systemctl daemon-reload
    printf 'service removed; programs, account, configuration, data and backups retained\n'
    exit 0
fi
if [[ "$action" == rollback ]]; then
    [[ "$2" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]*$ && "$2" != *..* ]] || die 'invalid release version'
    selected="$prefix/releases/$2"
    require_release "$selected"
    prepare_restore "$3"
else
    prepare_release "$2" "$3"
fi
prepare_unit
activate
