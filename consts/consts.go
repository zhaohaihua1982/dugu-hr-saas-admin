// Package consts 存放全局常量：枚举值、安全策略阈值、Redis key 规则与超时。
// 数值均来自 docs/SaaS管理后台-管理账号(sys_user)设计.md 的 §3~§8，修改须回对应章节核对。
package consts

import "time"

// ---------- 用户状态（sys_user.status） ----------
const (
	UserStatusActive   = 1 // 正常
	UserStatusDisabled = 2 // 停用
	UserStatusLocked   = 3 // 锁定（连续失败触发 frozen）
)

// SuperAdminUsername 内置超管账号名。
// 超管保护：不可停用、不可删除、必须密码登录；对其账号的一切操作强制审计（docs §8.3）。
// 设计文档的表结构未设超管标记列，以固定 username 识别。
const SuperAdminUsername = "admin"

// ---------- 登录类型 / 命中标识（sys_login_log） ----------
const (
	LoginTypePassword = "PASSWORD"
	LoginTypeSMS      = "SMS"

	IdentViaUsername = "USERNAME"
	IdentViaMobile   = "MOBILE"
)

// ---------- 失败原因固定枚举（sys_login_log.fail_reason，docs §3.2/§8） ----------
const (
	FailReasonBadCredential = "BAD_CREDENTIAL" // 凭证错误（含账号不存在，对外统一文案）
	FailReasonLocked        = "LOCKED"         // 锁定期内被拒
	FailReasonDisabled      = "DISABLED"       // 停用（因子校验通过后判定）
	FailReasonCaptchaFailed = "CAPTCHA_FAILED" // 图形验证码错误/未通过
	FailReasonSmsCodeError  = "SMS_CODE_ERROR" // 短信验证码错误
	FailReasonSmsExpired    = "SMS_EXPIRED"    // 短信验证码过期/已用
	FailReasonRateLimited   = "RATE_LIMITED"   // 触发限流
	FailReasonSendSkipped   = "SEND_SKIPPED"   // 防枚举：手机号未注册，假装已发送
)

// ---------- 安全策略阈值（docs §5/§6/§8） ----------
const (
	PasswordMinLength    = 10               // 密码最短 10 位
	PasswordStaleDays    = 180              // 密码超 180 天登录时提示（不强制）
	FailCountThreshold   = 5                // 连续错 5 次锁定
	LockDuration         = 15 * time.Minute // 锁 15 分钟；锁定期内尝试不重置计时
	LoginIPLimitMinute   = 20               // 登录接口 IP 20 次/分钟
	IPFailLimitHour      = 50               // IP 维度撞库兜底 50 次/小时
	CaptchaStreakTrigger = 3                // 密码登录连续失败 3 次后必须带图形码
	SmsExpireDuration    = 5 * time.Minute  // 短信验证码 5 分钟有效
	SmsMaxVerifyTries    = 5                // 单码试错 5 次作废
	SmsLength            = 6                // 6 位数字
	CaptchaExpire        = 5 * time.Minute  // 图形验证码有效时长
	CaptchaLength        = 5                // 图形验证码位数
	IdleSessionTTL       = 8 * time.Hour    // 会话 8 小时滑动（每次请求续期）
	HardSessionTTL       = 24 * time.Hour   // 硬上限 24 小时（防无损续期永不过期）
	SmsMinuteLimit       = 1 * time.Minute  // 单手机 1 条/分钟
	SmsMinuteLimitMax    = 1                // 单手机 1 条/分钟
	SmsDailyLimitMax     = 10               // 单手机 10 条/天
	SmsIPDailyLimitMax   = 100              // 单 IP 100 条/天
)

// ---------- Redis key 规则 ----------
// 用途隔离体现在 key 里（docs §5）：登录码不能用来改密码。
const (
	KeySession        = "sess:"             // sess:{jti}            → user_id，TTL 8h 滑动
	KeySessionAbs     = "sess:abs:"         // sess:abs:{jti}        → 硬上限标记，TTL 24h 不续期
	KeySessionUser    = "sess:u:"           // sess:u:{uid}          → SET(jti)，用于秒踢全端
	KeyCaptcha        = "captcha:"          // captcha:{captcha_id}  → 图形码答案，一次性
	KeyLoginIPLimit   = "login:limit:ip:"   // login:limit:ip:{ip}   → IP 登录限流计数
	KeyLoginStreakID  = "login:streak:uid:" // login:streak:uid:{uid}  → 连续失败步数（触发图形码）
	KeyLoginStreakKey = "login:streak:key:" // login:streak:key:{id_mask+ip} → 标识不存在的原账号限流
	KeyIPFail         = "login:ipfail:"     // login:ipfail:{ip}     → IP 撞库计数（50/h）
	KeyFailCount      = "fail:cnt:"         // fail:cnt:{uid}        → user_id 失败计数
	KeyFailLock       = "fail:lock:"        // fail:lock:{uid}       → 锁定标记，TTL 即剩余秒数

	// sms:code:{用途}:{手机hash} →HASH{code, tries}，TTL 5 分钟
	KeySmsCode    = "sms:code:%s:%s"
	KeySmsMinute  = "sms:min:%s:%s" // 单手机分钟限流
	KeySmsDaily   = "sms:day:%s:%s" // 单手机日限流
	KeySmsIPDaily = "sms:ipday:%s"  // 单 IP 日限流
)

// SmsPurpose 验证码用途（key 隔离，登录码不能用于改密）。
const (
	SmsPurposeLogin = "LOGIN"
	SmsPurposeReset = "RESET"
)
