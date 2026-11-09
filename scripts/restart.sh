#!/usr/bin/env bash
# restart.sh —— 重启（先优雅停止，再启动）
set -u
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
bash "$ROOT_DIR/scripts/stop.sh" || true
exec bash "$ROOT_DIR/scripts/start.sh"
