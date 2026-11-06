// 登录日志查询用例（docs §9 GET /login-logs）。
package application

import (
	"context"
	"time"

	"dugu-hr-saas-admin/domain/loginlog"
)

// LoginLogService 日志查询用例。
type LoginLogService struct {
	logs loginlog.Repository
}

// NewLoginLogService 构造。
func NewLoginLogService(logs loginlog.Repository) *LoginLogService {
	return &LoginLogService{logs: logs}
}

// List 日志查询（标识已按 docs §3.2 脱敏入库，不经手明文）。
func (s *LoginLogService) List(ctx context.Context, req *ListLoginLogsReq) (*ListLoginLogsResp, error) {
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
			ID:             l.ID,
			UserID:         l.UserID,
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
			return from, to, ErrIncorrectRange
		}
		from = t
	}
	if endAt != "" {
		t, err := time.ParseInLocation("2006-01-02", endAt, time.Local)
		if err != nil {
			return from, to, ErrIncorrectRange
		}
		to = t.Add(24*time.Hour - time.Millisecond) // 含当日
	}
	return from, to, nil
}
