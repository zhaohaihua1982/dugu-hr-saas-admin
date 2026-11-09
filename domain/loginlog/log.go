// Package loginlog —— 登录日志聚合（docs §3.2）。
// 三条使用规则沉淀为接口语义：
//  1. Append **写在事务外**（登录失败常伴随回滚，日志必须独立落库）；
//  2. 只存脱敏标识（identifier_mask），不存原文；
//  3. FailReason 是固定枚举，不是自由文本（自由文本没法做告警与统计）。
package loginlog

import (
	"context"
	"time"
)

// Type 登录方式（login_type）。
type Type string

// PASSWORD Password 登录入口；SMS 短信登录入口。
const (
	Password Type = "PASSWORD"
	SMS      Type = "SMS"
)

// Via 命中的标识（ident_via）。
type Via string

// USERNAME/IdentViaUsername MOBILE
const (
	ViaUsername Via = "USERNAME"
	ViaMobile   Via = "MOBILE"
)

// FailReason 失败原因固定枚举（docs §3.2/§8）。
type FailReason string

const (
	ReasonBadCredential FailReason = "BAD_CREDENTIAL" // 凭证错（含不存在，统一文案）
	ReasonLocked        FailReason = "LOCKED"         // 锁定期内被拒
	ReasonDisabled      FailReason = "DISABLED"       // 停用（因子通过后判定）
	ReasonCaptchaFailed FailReason = "CAPTCHA_FAILED" // 图形码错误
	ReasonSmsCodeError  FailReason = "SMS_CODE_ERROR" // 短信码错误
	ReasonSmsExpired    FailReason = "SMS_EXPIRED"    // 短信码过期/已用
	ReasonRateLimited   FailReason = "RATE_LIMITED"   // 限流
	ReasonSendSkipped   FailReason = "SEND_SKIPPED"   // 防枚举：未注册假装已发送
)

// Log 登录日志实体。user_id 解析失败时为 0。
type Log struct {
	ID             int64
	UserID         int64
	IdentifierMask string
	Type           Type
	Via            Via
	Success        bool
	Reason         FailReason
	IP             string
	UA             string
	At             time.Time
}

// Query 日志查询条件；TimeFrom/TimeTo 零值表示不限。
type Query struct {
	Page     int
	PageSize int
	UserID   int64
	IP       string
	// IdentifierMask 脱敏后标识的精确匹配。应用层把用户原始输入同规则脱敏后传入，
	// 调用方无需感知脱敏实现（命中 idx_log_identifier）。
	IdentifierMask string
	Success        *bool
	TimeFrom       time.Time
	TimeTo         time.Time
}

// Result 查询结果。
type Result struct {
	Items    []Log
	Total    int64
	Page     int
	PageSize int
}

// Repository 登录日志仓储端口（infrastructure/db/dao 实现）。
type Repository interface {
	// Append 独立落库（调用方保证不放进登录主流程的事务；失败仅告警）。
	Append(ctx context.Context, l *Log) error
	// Query 分页查询。
	Query(ctx context.Context, q Query) (*Result, error)
}
