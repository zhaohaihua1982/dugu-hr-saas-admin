// Package authn —— 认证域：会话、令牌、验证码、风险控制端口与领域错误。
// 标准DDD分层：本包只定义“业务需要什么能力”（端口/接口）与领域错误，
// 具体实现（JWT 签发、Redis 会话键、CAPTCHA 存储、SMS 通道、限流计数）由
// infrastructure 层提供（依赖倒置）。
package authn

import (
	"context"
	"errors"
	"time"
)

// ---------- 会话模型（docs §7） ----------

// 会话策略常量（docs §7）。
const (
	IssueTokenTTL   = 8 * time.Hour  // JWT 期满（配合 Redis 键滑动续期）
	HardSessionTTL  = 24 * time.Hour // 硬上限，杜绝靠“持续活动”永不过期
	LoginRateLimit  = 20             // 登录接口 IP 20 次/分钟（docs §8.6）
	IPFailLimitHour = 50             // IP 维度撞库兜底 50 次/小时（docs §6）
	StreakWindow    = time.Hour

	// CaptchaIssueLimits 图形验证码签发防刷（docs §5“短信轰炸是真实资损”的同源逻辑，
	// 图形码本身也是免费资源）：单 IP 10 次/分钟、60 次/小时；1 次/分钟兜底。
	CaptchaMinuteLimit = 10
	CaptchaHourLimit   = 60

	// SmsCodeLength 短信码定长 6 位数字（docs §5）——不达标直接判参数错误，
	// 不消耗 Redis 的一次性计数与试错窗口。
	SmsCodeLength = 6
)

// ErrBadCodeFormat 短信码不满足定长/字符集（请求体格式问题，非凭证错误）。
var ErrBadCodeFormat = errors.New("authn: sms code format invalid")

// ---------- 领域错误（应用层据此统一为业务码） ----------

var (
	ErrSessionNotFound = errors.New("authn: session not found") // 登出/被秒踢/过期
	ErrBadCredential   = errors.New("authn: bad credential")    // 统一口径：账号或密码错误
	ErrCaptchaRequired = errors.New("authn: captcha required")  // 连续失败达 StreakThreshold
	ErrCaptchaFailed   = errors.New("authn: captcha failed")
	ErrSmsCodeError    = errors.New("authn: sms code error")
	ErrSmsExpired      = errors.New("authn: sms code expired")
	ErrSmsSendFailed   = errors.New("authn: sms send failed")
	ErrRateLimited     = errors.New("authn: rate limited")
)

// ErrLocked 账号锁定错误，携带剩余秒数（docs §10：拒绝并返回 remaining_seconds）。
type ErrLocked struct{ Remaining time.Duration }

func (e *ErrLocked) Error() string { return "authn: locked" }

// ---------- 端口：令牌与会话 ----------

// Token JWT 签发结果。
type Token struct {
	Value string        // JWT 原文（Authorization: Bearer <token>）
	JTI   string        // 会话 ID，同时用于 Redis 会话键/秒踢
	TTL   time.Duration // 期满（docs §7：8h 滑动）
}

// TokenIssuer 令牌签发端口（infrastructure/token 实现）。
// 权限不进 token：只带 user_id 与 jti（docs §7）。
type TokenIssuer interface {
	Issue(ctx context.Context, uid int64) (Token, error)
}

// TokenVerifier 令牌校验端口（中间件用：解析+验签+过期校验）。
type TokenVerifier interface {
	Verify(ctx context.Context, raw string) (uid int64, jti string, err error)
}

// SessionStore 会话存储端口（Redis 实现）。
// 语义（docs §7）：Create 放置 sess:{jti}（8h 滑动 + 24h 硬上限）；
// Get 校验有效性并须与 JWT 载荷 uid 一致；Slide 每次请求续期；
// KickAll 改密/停用账号 → 全端秒踢。
type SessionStore interface {
	Create(ctx context.Context, uid int64, jti string) error
	Get(ctx context.Context, jti string) (uid int64, err error)
	Slide(ctx context.Context, jti string) error
	Delete(ctx context.Context, uid int64, jti string) error
	KickAll(ctx context.Context, uid int64) error
}

// ---------- 端口：验证码与短信 ----------

// Captcha 图形验证码（docs §5 前置、§8.7 密码连续失败 3 次后必带）。
type Captcha struct {
	ID    string
	Image string // data:image/svg+xml;base64,...
}

// CaptchaService 图形验证码端口（infrastructure 实现：生成 + 一次性存储）。
type CaptchaService interface {
	Issue(ctx context.Context) (Captcha, error)
	// Verify 一次性：用后即删，防重放。
	Verify(ctx context.Context, id, answer string) (bool, error)
}

// SmsSender 短信发送端口（infrastructure 实现；开发环境为日志桩）。
type SmsSender interface {
	Send(ctx context.Context, mobile, code string) error
}

// SmsPurpose 验证码用途（key 隔离：登录码不能用来改密码，docs §5）。
type SmsPurpose string

// PurposeLogin / PurposeReset 用途枚举。
const (
	PurposeLogin SmsPurpose = "LOGIN"
	PurposeReset SmsPurpose = "RESET"
)

// _smsCodeResult 短信码校验结果。
type SmsCodeResult int

// CodeResult 枚举（与仓储实现取值约定一致）。
const (
	CodeOK        SmsCodeResult = iota // 命中（一次性：用后即删）
	CodeMismatch                       // 码不对（尝试 +1）
	CodeExhausted                      // 试错 5 次作废 → 引导重发
	CodeNoCode                         // 过期/不存在
)

// SmsCodeStore 短信验证码存储端口（Redis 实现，docs §5：
// 6 位数字、5 分钟有效、一次性、试错 5 次作废）。
type SmsCodeStore interface {
	Save(ctx context.Context, p SmsPurpose, mobileHash, code string) error
	Verify(ctx context.Context, p SmsPurpose, mobileHash, code string) (SmsCodeResult, error)
}

// ---------- 端口：风险控制（失败计数/锁定/限流，docs §5/§6/§8.6） ----------

// LoginGuard IP 维度限流与配额（含图形验证码签发防刷）。
// Redis 故障时实现保守拒绝/告警放行由 application 决策，端口只报错。
type LoginGuard interface {
	// AllowCaptcha 图形验证码签发限流（10/min、60/h）。
	AllowCaptcha(ctx context.Context, ip string) error
	// AllowLogin 登录接口 IP 限流（20/min）。
	AllowLogin(ctx context.Context, ip string) error
	// AllowIPFail IP 撞库兜底（50/h）；
	AllowIPFail(ctx context.Context, ip string) error
	// RecordIPFail 登录失败计数（u 存在与否都计入）。
	RecordIPFail(ctx context.Context, ip string) error
	// AllowSms 短信发送配额（单手机 1/min、10/day；单 IP 100/day，docs §5）。
	AllowSms(ctx context.Context, p SmsPurpose, mobileHash, ip string) error
}

// LockGuard user 维度失败计数与锁定（Redis 加速层；DB 兜底由 sysuser.Repository）。
type LockGuard interface {
	// Remaining 当前锁定剩余时长；未锁定返回 0。语义同 docs §6：绝不因 Redis 故障放行（错误上抛）。
	Remaining(ctx context.Context, uid int64) (time.Duration, error)
	// IncrFail 失败计数 +1，返回新值。
	IncrFail(ctx context.Context, uid int64) (int, error)
	// MarkLocked 写锁定标记，TTL=锁定时长（锁定期内尝试不重置计时）。
	MarkLocked(ctx context.Context, uid int64, d time.Duration) error
	// ResetFail 成功登录后清零（含 streak）。
	ResetFail(ctx context.Context, uid int64, idMask, ip string) error
	// IncrStreak 连续失败步数（uid 维度，触发 Captcha）。
	IncrStreak(ctx context.Context, uid int64) (int, error)
	// GetStreak 读取 uid 维度步数。
	GetStreak(ctx context.Context, uid int64) (int, error)
	// IncrStreakByKey 标识不存在时按“标识+IP”兜底计数（docs §10）。
	IncrStreakByKey(ctx context.Context, idMask, ip string) (int, error)
}

// IDGenerator 雪花 ID 生成端口。
type IDGenerator interface {
	New() int64
}
