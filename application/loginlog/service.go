// Package loginlog —— 登录日志查询用例（docs §9 GET /login-logs）。
package loginlog

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"dugu-hr-saas-admin/domain/loginlog"
)

// ---------- 用例输入/输出对象 ----------

// LoginLogResp 登录日志条目（id/user_id 用字符串：雪花 ID 超出 JS 安全整数，见 UserView 注释）。
type LoginLogResp struct {
	ID             string              `json:"id"`
	UserID         string              `json:"user_id"`
	IdentifierMask string              `json:"identifier_mask"`
	Type           string              `json:"login_type"`
	Via            string              `json:"ident_via"`
	Success        bool                `json:"success"`
	Reason         loginlog.FailReason `json:"fail_reason"`
	IP             string              `json:"ip"`
	UA             string              `json:"ua"`
	At             string              `json:"created_at"`
}

// ListLoginLogsReq 登录日志查询（StartAt/EndAt 为 YYYY-MM-DD，空串不限）。
type ListLoginLogsReq struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	UserID   int64  `form:"user_id"`
	IP       string `form:"ip"`
	Success  *bool  `form:"success"`
	StartAt  string `form:"start_at"`
	EndAt    string `form:"end_at"`
}

// ListLoginLogsResp 登录日志响应。
type ListLoginLogsResp struct {
	Items    []LoginLogResp `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// ErrBadRange 时间区间格式不合法（映射 code.InvalidParams）。
var ErrBadRange = fmt.Errorf("loginlog: bad time range")

// ---------- 用例 ----------

// Service 日志查询用例。
type Service struct {
	logs loginlog.Repository
}

// NewService 构造。
func NewService(logs loginlog.Repository) *Service { return &Service{logs} }

// List 日志查询（标识已按 docs §3.2 脱敏入库，不经手明文）。
func (s *Service) List(ctx context.Context, req *ListLoginLogsReq) (*ListLoginLogsResp, error) {
	from, to, err := parseRange(req.StartAt, req.EndAt)
	if err != nil {
		return nil, err
	}
	res, err := s.logs.Query(ctx, loginlog.Query{
		Page:     req.Page,
		PageSize: req.PageSize,
		UserID:   req.UserID,
		IP:       req.IP,
		Success:  req.Success,
		TimeFrom: from,
		TimeTo:   to,
	})
	if err != nil {
		return nil, err
	}
	items := make([]LoginLogResp, 0, len(res.Items))
	for i := range res.Items {
		l := &res.Items[i]
		items = append(items, LoginLogResp{
			ID:             strconv.FormatInt(l.ID, 10),
			UserID:         strconv.FormatInt(l.UserID, 10),
			IdentifierMask: l.IdentifierMask,
			Type:           string(l.Type),
			Via:            string(l.Via),
			Success:        l.Success,
			Reason:         l.Reason,
			IP:             l.IP,
			UA:             l.UA,
			At:             l.At.Format(time.DateTime),
		})
	}
	return &ListLoginLogsResp{Items: items, Total: res.Total, Page: res.Page, PageSize: res.PageSize}, nil
}

// parseRange 解析 YYYY-MM-DD（空串不限）。
func parseRange(startAt, endAt string) (time.Time, time.Time, error) {
	var from, to time.Time
	if startAt != "" {
		t, err := time.ParseInLocation("2006-01-02", startAt, time.Local)
		if err != nil {
			return from, to, ErrBadRange
		}
		from = t
	}
	if endAt != "" {
		t, err := time.ParseInLocation("2006-01-02", endAt, time.Local)
		if err != nil {
			return from, to, ErrBadRange
		}
		to = t.Add(24*time.Hour - time.Millisecond) // 含当日
	}
	return from, to, nil
}
