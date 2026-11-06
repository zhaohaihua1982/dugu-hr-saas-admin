// service/login_log.go —— 登录日志查询（docs §9 GET /login-logs）。
package service

import (
	"context"
	"time"

	"dugu-hr-saas-admin/repository/db/dao"
	"dugu-hr-saas-admin/types"
)

// LoginLogSrv 登录日志业务。
type LoginLogSrv struct{}

// GetLoginLogSrv 单例（gin-mall 风格）。
func GetLoginLogSrv() *LoginLogSrv { return &LoginLogSrv{} }

// List 登录日志查询（日志从 sys_login_log 常驻表检索，标识已按 §3.2 脱敏入库）。
func (s *LoginLogSrv) List(ctx context.Context, req *types.ListLoginLogsReq) (*types.ListLoginLogsResp, error) {
	q := dao.LogListQuery{
		Page:     req.Page,
		PageSize: req.PageSize,
		UserId:   req.UserId,
		Ip:       req.Ip,
		Success:  req.Success,
	}
	start, end, err := parseRange(req.StartAt, req.EndAt)
	if err != nil {
		return nil, err
	}
	q.StartAt, q.EndAt = start, end

	res, err := dao.NewSysLoginLogDao(ctx).List(q)
	if err != nil {
		return nil, err
	}
	items := make([]types.LoginLogResp, 0, len(res.Items))
	for i := range res.Items {
		l := &res.Items[i]
		items = append(items, types.LoginLogResp{
			Id:             l.Id,
			UserId:         l.UserId,
			IdentifierMask: l.IdentifierMask,
			LoginType:      l.LoginType,
			IdentVia:       l.IdentVia,
			Success:        l.Success,
			FailReason:     l.FailReason,
			Ip:             l.Ip,
			Ua:             l.Ua,
			CreatedAt:      l.CreatedAt.Format(time.DateTime),
		})
	}
	return &types.ListLoginLogsResp{
		Items:    items,
		Total:    res.Total,
		Page:     res.Page,
		PageSize: res.PageSize,
	}, nil
}

// parseRange 解析 YYYY-MM-DD 时间范围（空串 = 不限制）。
func parseRange(startAt, endAt string) (time.Time, time.Time, error) {
	var start, end time.Time
	if startAt != "" {
		t, err := time.ParseInLocation("2006-01-02", startAt, time.Local)
		if err != nil {
			return start, end, ErrIncorrectRange
		}
		start = t
	}
	if endAt != "" {
		t, err := time.ParseInLocation("2006-01-02", endAt, time.Local)
		if err != nil {
			return start, end, ErrIncorrectRange
		}
		end = t.Add(24*time.Hour - time.Millisecond) // 含当日
	}
	return start, end, nil
}
