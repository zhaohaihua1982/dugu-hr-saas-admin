// cache 端口实现的辅助值（docs §5/§6 常量在本层具化）。
package cache

import (
	"strconv"
	"time"

	"dugu-hr-saas-admin/domain/authn"
)

// 短信配额常量（docs §5：单手机 1 条/分钟、10 条/天；单 IP 100 条/天）。
const (
	minuteLimitWindow = time.Minute
	dayLimitWindow    = 24 * time.Hour // 一律 24h 固定窗口，不做自然日时区边界
	minuteLimitMax    = int64(1)
	dailyLimitMax     = int64(10)
	ipDailyLimitMax   = int64(100)
)

// captchaTTL 图形码有效时长（docs §5：5 分钟）。
func captchaTTL() time.Duration { return 5 * time.Minute }

// maxSmsTries 单验证码试错上限（错满作废，docs §5：6 位码只有 100 万种）。
func maxSmsTries() int { return 5 }

// smsCodeKey sms:code:{用途}:{手机hash}（用途隔离：登录码不能用来改密码，docs §5）。
func smsCodeKey(p authn.SmsPurpose, mobileHash string) string {
	return "sms:code:" + string(p) + ":" + mobileHash
}

// smsMinuteKey 单手机分钟限流键。
func smsMinuteKey(p authn.SmsPurpose, mobileHash string) string {
	return "sms:min:" + string(p) + ":" + mobileHash
}

// smsDayKey 单手机日限流键。
func smsDayKey(p authn.SmsPurpose, mobileHash string) string {
	return "sms:day:" + string(p) + ":" + mobileHash
}

// smsIPDayKey 单 IP 日限流键。
func smsIPDayKey(ip string) string { return "sms:ipday:" + ip }

// fmtInt int64 → string。
func fmtInt(v int64) string { return strconv.FormatInt(v, 10) }
