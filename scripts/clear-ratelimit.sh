#!/usr/bin/env bash
# clear-ratelimit.sh —— 清除“某个 IP”的限流计数器（开发调试用，生产禁用）
#   用法：
#     bash scripts/clear-ratelimit.sh                # 清本机 127.0.0.1
#     bash scripts/clear-ratelimit.sh 192.168.1.8    # 清指定 IP
#     bash scripts/clear-ratelimit.sh --all          # 清所有 IP 的限流计数（慎用）
#   说明：不清验证码/短信码本体（captcha:{id}、sms:code:*）——那些是“资源”，
#         只清防刷计数器（captcha:limit:*、login:*、fail:*、sms:ipday:*）。
set -u

IP="${1:-127.0.0.1}"
REDIS_DB=4
redis() { redis-cli -n "$REDIS_DB" "$@"; }

if [ "$IP" = "--all" ]; then
    patterns=("*:limit:*" "login:*" "fail:*" "sms:ipday:*" "sms:min:*" "sms:day:*")
else
    patterns=(
        "captcha:limit:${IP}:*"
        "login:limit:ip:${IP}"
        "login:ipfail:${IP}"
        "login:limit:ip:::${IP}"   # ::1 形式的本机 IPv6
        "login:streak:key:*@${IP}"
        "login:streak:uid:*"
        "fail:lock:*"
        "fail:cnt:*"
        "sms:ipday:${IP}"
    )
fi

count=0
for pat in "${patterns[@]}"; do
    ks=$(redis --scan --pattern "$pat" 2>/dev/null)
    for k in $ks; do
        redis DEL "$k" >/dev/null
        count=$((count + 1))
    done
done

echo "✅ 已删除 $count 个限流计数键（db=$REDIS_DB，scope=${IP}；验证码/短信码本体未动）"
