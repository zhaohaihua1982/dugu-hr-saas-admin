package types

// LoginLogResp 登录日志条目（对外响应，不含 ip/ua 等敏感细节的控制不变——
// 管理后台日志查询本身即运营功能，ip/ua 属于运营可见字段）。
type LoginLogResp struct {
	Id             int64  `json:"id"`
	UserId         int64  `json:"user_id"`
	IdentifierMask string `json:"identifier_mask"`
	LoginType      string `json:"login_type"`
	IdentVia       string `json:"ident_via"`
	Success        bool   `json:"success"`
	FailReason     string `json:"fail_reason"`
	Ip             string `json:"ip"`
	Ua             string `json:"ua"`
	CreatedAt      string `json:"created_at"`
}

// ListLoginLogsReq 登录日志查询（GET /login-logs）。
type ListLoginLogsReq struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	UserId   int64  `form:"user_id"`
	Ip       string `form:"ip"`
	Success  *bool  `form:"success"`
	StartAt  string `form:"start_at"` // YYYY-MM-DD（本地时区）或 RFC3339
	EndAt    string `form:"end_at"`
}

// ListLoginLogsResp 登录日志响应。
type ListLoginLogsResp struct {
	Items    []LoginLogResp `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}
