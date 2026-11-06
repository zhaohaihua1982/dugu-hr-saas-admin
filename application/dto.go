// Package application —— 应用层：编排领域逻辑的用例服务。
// 依赖只有 domain（端口+聚合），不直接依赖 gin/gorm/redis。
// HTTP 数据结构、错误码联动见 dto.go / errors.go。
package application

import (
	"dugu-hr-saas-admin/domain/loginlog"
	"dugu-hr-saas-admin/domain/sysuser"
	"time"
)

// RequestParams 接口层传入的请求上下文参数（ip/ua 由 handler 提取）。
type RequestParams struct {
	IP string
	UA string
}

// AuthDTO 应用层输入/输出对象（接口层直接绑定）。

// CaptchaGetResp 图形验证码响应。
type CaptchaGetResp struct {
	CaptchaID string `json:"captcha_id"`
	Image     string `json:"image"`
}

// SendSmsReq 发送登录短信（docs §9：需图形码）。
type SendSmsReq struct {
	Mobile      string `json:"mobile" binding:"required"`
	CaptchaID   string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
}

// LoginPasswordReq 账号或手机 + 密码（CaptchaID/Code 连续失败达阈值后必带，docs §8.7）。
type LoginPasswordReq struct {
	Identifier  string `json:"identifier" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
}

// LoginSmsReq 手机 + 短信码。
type LoginSmsReq struct {
	Mobile string `json:"mobile" binding:"required"`
	Code   string `json:"code" binding:"required"`
}

// UserView 对外展示视图（手机只用 mask，永不解密——docs §3.3①）。
type UserView struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Mobile    string `json:"mobile"`
	Status    int    `json:"status"`
	CreatedAt string `json:"created_at"`
}

// LoginResp 登录成功响应（token + 用户视图 + 改密提示）。
type LoginResp struct {
	Token         string   `json:"token"`
	ExpiresIn     int64    `json:"expires_in"`
	User          UserView `json:"user"`
	PasswordStale bool     `json:"password_stale"`
}

// CreateSysUserReq 创建管理账号（docs §9）。
type CreateSysUserReq struct {
	Username string `json:"username" binding:"required,max=64"`
	Name     string `json:"name" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=64"`
	Mobile   string `json:"mobile" binding:"required"`
	Email    string `json:"email" binding:"omitempty,email,max=128"`
}

// UpdateSysUserReq 修改账号（	username 不可改；status 仅 1/2；mobile 传入即换绑）。
type UpdateSysUserReq struct {
	Name   string          `json:"name" binding:"omitempty,max=64"`
	Email  string          `json:"email" binding:"omitempty,email,max=128"`
	Mobile string          `json:"mobile" binding:"omitempty"`
	Status *sysuser.Status `json:"status" binding:"omitempty,oneof=1 2"`
}

// SysUserListReq 账号列表。
type SysUserListReq struct {
	Page     int             `form:"page" binding:"omitempty,min=1"`
	PageSize int             `form:"page_size" binding:"omitempty,min=1,max=100"`
	Keyword  string          `form:"keyword"`
	Status   *sysuser.Status `form:"status" binding:"omitempty,oneof=1 2 3"`
}

// SysUserListResp 账号列表响应。
type SysUserListResp struct {
	Items    []UserView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// ResetPasswordReq 重置密码（强审计）。
type ResetPasswordReq struct {
	Password string `json:"password" binding:"required,max=64"`
}

// LoginLogResp 登录日志条目。
type LoginLogResp struct {
	ID             int64               `json:"id"`
	UserID         int64               `json:"user_id"`
	IdentifierMask string              `json:"identifier_mask"`
	Type           string              `json:"login_type"`
	Via            string              `json:"ident_via"`
	Success        bool                `json:"success"`
	Reason         loginlog.FailReason `json:"fail_reason"`
	IP             string              `json:"ip"`
	UA             string              `json:"ua"`
	At             string              `json:"created_at"`
}

// ListLoginLogsReq 登录日志查询。
type ListLoginLogsReq struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	UserID   int64  `form:"user_id"`
	IP       string `form:"ip"`
	Success  *bool  `form:"success"`
	StartAt  string `form:"start_at"` // YYYY-MM-DD
	EndAt    string `form:"end_at"`
}

// ListLoginLogsResp 登录日志响应。
type ListLoginLogsResp struct {
	Items    []LoginLogResp `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// toUserView 聚合 → 展示视图。
func toUserView(u *sysuser.User) UserView {
	v := UserView{
		ID:       u.ID,
		Username: u.Username,
		Name:     u.Name,
		Email:    u.Email,
		Mobile:   u.Mobile.Mask,
		Status:   int(u.Status),
	}
	if !u.CreatedAt.IsZero() {
		v.CreatedAt = u.CreatedAt.Format(time.DateTime)
	}
	return v
}
