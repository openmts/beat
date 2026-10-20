#!/usr/bin/env bash
# 仅供没有 docker 的本地宿主机运行 `scripts/check.sh container` 时垫片使用，
# 用法：PATH="$(pwd)/scripts/local:$PATH" 且把本文件软/硬链接为 `docker`，
# 或者 `PATH=$(mktemp -d)&&install -m0700 scripts/local/docker-podman-shim.sh $PATH/docker`。
# CI 使用真 docker，不经过本脚本。
set -euo pipefail

# docker 的 tmpfs 支持 uid=/gid=，podman 不支持；剥离这两个键并把 owner-only 模式
# 放宽为 1777，让镜像里的 uid 65532 仍能写入。容器仍以镜像 USER 运行，隔离意图不变。
translate_tmpfs() {
    local value=$1
    value=$(printf '%s' "$value" | sed -E 's/(^|,)(uid|gid)=[0-9]+//g')
    value=${value//mode=0700/mode=1777}
    printf '%s' "$value"
}

if [[ "${1-}" == "build" ]]; then
    shift
    # buildah 的构建容器默认 /dev/shm 只有 64 MiB，原生构建工具会因此崩溃。
    exec podman build --shm-size=1g "$@"
fi

args=()
expect_tmpfs=0
for arg in "$@"; do
    if ((expect_tmpfs)); then
        args+=("$(translate_tmpfs "$arg")")
        expect_tmpfs=0
        continue
    fi
    args+=("$arg")
    [[ "$arg" == "--tmpfs" ]] && expect_tmpfs=1
done
exec podman "${args[@]}"
