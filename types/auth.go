// Package types 请求/响应 DTO。校验规则用 binding 标签（gin 内置 validator）。
package types

// CaptchaGetResp 图形验证码响应。
type CaptchaGetResp struct {
	CaptchaId string `json:"captcha_id"`
	Image     string `json:"image"` // data:image/svg+xml;base64,...
}

// GetCaptchaReq 获取图形验证码（占位：无入参，保留扩展点如场景 scene）。
type GetCaptchaReq struct{}

// SendSmsReq 发送登录短信（docs §9：需图形码）。
type SendSmsReq struct {
	Mobile      string `json:"mobile" binding:"required"`
	CaptchaId   string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
}

// LoginPasswordReq 账号或手机 + 密码（docs §4.2）。
// CaptchaId/CaptchaCode：连续失败达阈值后必须携带（§8.7）。
type LoginPasswordReq struct {
	Identifier  string `json:"identifier" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaId   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
}

// LoginSmsReq 手机 + 短信码。
type LoginSmsReq struct {
	Mobile string `json:"mobile" binding:"required"`
	Code   string `json:"code" binding:"required"`
}

// UserView 对外展示视图（手机只用 mask，永不解密）。
type UserView struct {
	Id        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Mobile    string `json:"mobile"` // 138****8000
	Status    int    `json:"status"`
	CreatedAt string `json:"created_at"`
}

// LoginResp 登录成功响应（docs §7：JWT + Redis 会话）。
type LoginResp struct {
	Token         string   `json:"token"` // Authorization: Bearer <token>
	ExpiresIn     int64    `json:"expires_in"`
	User          UserView `json:"user"`
	PasswordStale bool     `json:"password_stale"` // 密码超 180 天，提示改密（不强制）
}
