// Package e 业务错误码。编号按“模块×10000”分段，与 gin-mall 的 pkg/e 同风格；
// 30000 段保留给 SaaS 管理后台认证体系。
package code

const (
	SUCCESS       = 200
	UpdateSuccess = 201
	ERROR         = 500
	InvalidParams = 400

	// ---------- 认证（30001~30030） ----------
	ErrorAuthCheckTokenFail = 30001 // token 无效/过期/会话失效
	ErrorAuthRequired       = 30002 // 未登录
	ErrorBadCredential      = 30010 // 账号或密码错误（统一文案，不区分细分原因）
	ErrorUserLocked         = 30011 // 账号锁定中（data.remaining_seconds）
	ErrorUserDisabled       = 30012 // 账号已停用
	ErrorCaptchaFailed      = 30013 // 图形验证码错误或已过期
	ErrorCaptchaRequired    = 30014 // 需要图形验证码（连续失败达阈值）
	ErrorSmsCodeError       = 30015 // 短信验证码错误
	ErrorSmsExpired         = 30016 // 短信验证码过期/已用
	ErrorRateLimited        = 30017 // 请求过于频繁
	ErrorSendSms            = 30018 // 短信发送失败

	// ---------- 管理操作（30021~30040） ----------
	ErrorUserNameTaken       = 30021 // 账号已存在
	ErrorMobileTaken         = 30022 // 手机号已绑定其他账号
	ErrorSysUserNotFound     = 30023 // 管理账号不存在
	ErrorWeakPassword        = 30024 // 密码不满足策略
	ErrorBadIdentifierFormat = 30025 // 账号/手机号格式不合法
	ErrorSuperAdminProtected = 30026 // 超管账号受保护，禁止该操作
	ErrorSelfOperation       = 30027 // 不能对当前登录账号自身执行停用/启用、解锁等操作

	// ---------- 数据库（40001） ----------
	ErrorDatabase = 40001
)
