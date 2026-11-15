#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "beat-server installation must run as root" >&2
  exit 1
fi

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN_DIR="$ROOT_DIR/bin"

if [ ! -x "$BIN_DIR/beat-server" ]; then
  echo "missing $BIN_DIR/beat-server; run 'make build' first" >&2
  exit 1
fi

if [ "${BEAT_START_NOW:-true}" = "true" ]; then
  "$BIN_DIR/beat-server" install
else
  "$BIN_DIR/beat-server" install --no-start
fi
