package types

// CreateSysUserReq 创建管理账号（docs §9 POST /sys-users）。
// username 禁止 11 位纯数字（§3.3③）；密码须 ≥10 位含两类字符（§8.2）。
type CreateSysUserReq struct {
	Username string `json:"username" binding:"required,max=64"`
	Name     string `json:"name" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=64"`
	Mobile   string `json:"mobile" binding:"required"`
	Email    string `json:"email" binding:"omitempty,email,max=128"`
}

// UpdateSysUserReq 修改账号（PUT /sys-users/{id}）。
// status 仅允许 1(正常)/2(停用)，锁定态由系统安全策略维护、不允许手改；
// mobile 传入即换绑（三列一起更新）；username 不可改（登录解析锚点）。
type UpdateSysUserReq struct {
	Name   string `json:"name" binding:"omitempty,max=64"`
	Email  string `json:"email" binding:"omitempty,email,max=128"`
	Mobile string `json:"mobile" binding:"omitempty"`
	Status *int   `json:"status" binding:"omitempty,oneof=1 2"`
}

// SysUserListReq 账号列表（GET /sys-users）。
type SysUserListReq struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Keyword  string `form:"keyword"` // 姓名/账号模糊
	Status   *int   `form:"status" binding:"omitempty,oneof=1 2 3"`
}

// SysUserListResp 账号列表响应。
type SysUserListResp struct {
	Items    []UserView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// ResetPasswordReq 重置密码（POST /sys-users/{id}/reset-password，强审计）。
type ResetPasswordReq struct {
	Password string `json:"password" binding:"required,max=64"`
}
