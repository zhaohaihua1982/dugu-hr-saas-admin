#!/usr/bin/env bash
# stop.sh —— 优雅停止 dugu-hr-saas-admin 服务
#   行为：SIGTERM（触发服务内置的优雅关停：等存量请求处理完，最多 5s）；
#         超过 10s 未退出时 SIGKILL 强杀兜底。
set -u

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PID_FILE="$ROOT_DIR/logs/saas-admin.pid"

if [ ! -f "$PID_FILE" ]; then
    echo "无 PID 文件，尝试按二进制路径匹配进程..."
    PIDS=$(pgrep -f "$ROOT_DIR/build/saas-admin" 2>/dev/null)
    [ -z "$PIDS" ] && { echo "服务未在运行"; exit 0; }
    echo "匹配到进程: $PIDS"
else
    PIDS=$(cat "$PID_FILE")
fi

for pid in $PIDS; do
    if ! kill -0 "$pid" 2>/dev/null; then
        echo "pid=$pid 已不存在，清理 PID 文件"
        rm -f "$PID_FILE"
        continue
    fi
    echo "向 pid=$pid 发送 SIGTERM（优雅关停，最多 10s）..."
    kill -TERM "$pid"
    for _ in $(seq 1 20); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.5
    done
    if kill -0 "$pid" 2>/dev/null; then
        echo "优雅关停超时，SIGKILL 强杀..."
        kill -9 "$pid"
    fi
done

rm -f "$PID_FILE"
# 确认端口释放
sleep 0.5
if command -v lsof >/dev/null 2>&1 && lsof -ti :${SAAS_PORT:-8080} >/dev/null 2>&1; then
    echo "⚠️ 端口仍被占用（可能还有其他实例）: lsof -i :${SAAS_PORT:-8080}"
else
    echo "✅ 服务已停止"
fi
