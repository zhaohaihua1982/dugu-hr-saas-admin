// loginlog.Repository 的实现（docs §3.2 规则沉淀在接口语义内）。
package dao

import (
	"context"
	"time"

	"dugu-hr-saas-admin/domain/loginlog"
	"dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/po"
)

// LoginLogRepository 登录日志仓储实现。
type LoginLogRepository struct{ d *db.DB }

// NewLoginLogRepository 构造。
func NewLoginLogRepository(d *db.DB) *LoginLogRepository { return &LoginLogRepository{d} }

// Append 独立落库（不放进登录主流程事务；失败仅告警）。
func (r *LoginLogRepository) Append(ctx context.Context, l *loginlog.Log) error {
	p := &po.SysLoginLog{
		ID:             l.ID,
		UserID:         l.UserID,
		IdentifierMask: l.IdentifierMask,
		LoginType:      string(l.Type),
		IdentVia:       string(l.Via),
		Success:        l.Success,
		FailReason:     string(l.Reason),
		IP:             l.IP,
		UA:             l.UA,
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	l.At = p.CreatedAt
	return r.d.WithContext(ctx).Create(p).Error
}

// Query 分页查询（索引已按 user/time/ip 三路覆盖）。
func (r *LoginLogRepository) Query(ctx context.Context, q loginlog.Query) (*loginlog.Result, error) {
	page, size := normalizePage(q.Page, q.PageSize)
	d := r.d.WithContext(ctx).Model(&po.SysLoginLog{})
	if q.UserID > 0 {
		d = d.Where("user_id = ?", q.UserID)
	}
	if q.IP != "" {
		d = d.Where("ip = ?", q.IP)
	}
	if q.Success != nil {
		d = d.Where("success = ?", *q.Success)
	}
	if !q.TimeFrom.IsZero() {
		d = d.Where("created_at >= ?", q.TimeFrom)
	}
	if !q.TimeTo.IsZero() {
		d = d.Where("created_at <= ?", q.TimeTo)
	}
	var total int64
	if err := d.Count(&total).Error; err != nil {
		return nil, err
	}
	var items []po.SysLoginLog
	if err := d.Order("id DESC").Limit(size).Offset((page - 1) * size).Find(&items).Error; err != nil {
		return nil, err
	}
	out := make([]loginlog.Log, 0, len(items))
	for i := range items {
		p := &items[i]
		out = append(out, loginlog.Log{
			ID: p.ID, UserID: p.UserID,
			IdentifierMask: p.IdentifierMask,
			Type:           loginlog.Type(p.LoginType),
			Via:            loginlog.Via(p.IdentVia),
			Success:        p.Success,
			Reason:         loginlog.FailReason(p.FailReason),
			IP:             p.IP, UA: p.UA, At: p.CreatedAt,
		})
	}
	return &loginlog.Result{Items: out, Total: total, Page: page, PageSize: size}, nil
}
