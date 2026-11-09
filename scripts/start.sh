#!/usr/bin/env bash
# start.sh —— 后台启动 dugu-hr-saas-admin 服务
#   用法：bash scripts/start.sh   （可先配 SAAS_* 环境变量，如 SAAS_ADDR=:8080）
#   行为：
#     1. 已在运行则直接退出（幂等，以 PID 文件为准）
#     2. 未传 SKIP_BUILD=1 时先编译到 build/saas-admin
#     3. 以项目根目录为工作目录启动（config/locales 相对路径依赖），nohup 后台运行
#     4. 轮询 /healthz 直至就绪（默认等 15s），失败则打印日志尾部帮助排查
set -u

# 项目根目录 = scripts/ 的上一级；启动必须 cd 到根（config 相对路径）
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="$ROOT_DIR/logs"
PID_FILE="$LOG_DIR/saas-admin.pid"
BIN="$ROOT_DIR/build/saas-admin"
LOG_FILE="$LOG_DIR/run-$(date +%F).log"
HEALTH_URL="${SAAS_ADDR:-}:8080"
HEALTH_URL="http://127.0.0.1${HEALTH_URL#\*:}/healthz"   # :8080 → http://127.0.0.1:8080/healthz
READY_WAIT_SEC=15

mkdir -p "$LOG_DIR"

is_running() {
    [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

if is_running; then
    echo "服务已在运行（pid=$(cat "$PID_FILE")），无需重复启动。如需重启：bash scripts/restart.sh"
    exit 0
fi
rm -f "$PID_FILE"

cd "$ROOT_DIR" || exit 1

# 构建（SKIP_BUILD=1 跳过，直接用现有二进制）
if [ "${SKIP_BUILD:-0}" != "1" ]; then
    echo "[1/3] 编译 ./cmd -> build/saas-admin ..."
    go build -o "$BIN" ./cmd || { echo "编译失败"; exit 1; }
else
    echo "[1/3] SKIP_BUILD=1，跳过编译"
    [ -x "$BIN" ] || { echo "未找到二进制 $BIN"; exit 1; }
fi

# 启动（nohup + setsid 脱离终端；日志按天切分）
echo "[2/3] 启动服务（工作目录=$ROOT_DIR，日志=$LOG_FILE）..."
nohup "$BIN" >> "$LOG_FILE" 2>&1 &
echo $! > "$PID_FILE"
disown $!

# 健康检查：/healthz 就绪才算启动成功
echo "[3/3] 等待就绪（$HEALTH_URL，最多 ${READY_WAIT_SEC}s）..."
for _ in $(seq 1 $((READY_WAIT_SEC * 2))); do
    if curl -sf -o /dev/null "$HEALTH_URL"; then
        echo "✅ 服务已就绪: $HEALTH_URL（pid=$(cat "$PID_FILE")，日志: tail -f $LOG_FILE）"
        exit 0
    fi
    if ! kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
        echo "❌ 进程已退出，日志尾部："
        tail -20 "$LOG_FILE"
        rm -f "$PID_FILE"
        exit 1
    fi
    sleep 0.5
done
echo "❌ ${READY_WAIT_SEC}s 内未就绪（启动可能卡在依赖），日志尾部："
tail -20 "$LOG_FILE"
kill "$(cat "$PID_FILE")" 2>/dev/null
rm -f "$PID_FILE"
exit 1
