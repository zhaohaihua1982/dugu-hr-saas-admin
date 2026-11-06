package dao

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"dugu-hr-saas-admin/pkg/utils/uid"
	"dugu-hr-saas-admin/repository/db/model"
)

// SysLoginLogDao sys_login_log 表仓储。使用规则（docs §3.2）：
// 写事务外（登录失败常伴随回滚，日志必须独立落库）；存脱敏标识；fail_reason 固定枚举。
type SysLoginLogDao struct{ ctx context.Context }

// NewSysLoginLogDao 以请求 ctx 构造仓储。
func NewSysLoginLogDao(ctx context.Context) *SysLoginLogDao { return &SysLoginLogDao{ctx} }

func (d *SysLoginLogDao) db() *gorm.DB { return NewDBClient(d.ctx) }

// Create 独立落库一条日志（调用方保证：不放进登录主流程的事务；失败只告警不影响登录结果）。
func (d *SysLoginLogDao) Create(l *model.SysLoginLog) error {
	if l.Id == 0 {
		l.Id = uid.New()
	}
	l.CreatedAt = time.Now()
	if err := d.db().Create(l).Error; err != nil {
		return fmt.Errorf("sys_login_log.create: %w", err)
	}
	return nil
}

// LogListQuery 登录日志查询条件。
type LogListQuery struct {
	Page     int
	PageSize int
	UserId   int64
	Ip       string
	Success  *bool
	// 时间范围（含）；零值表示不限制
	StartAt time.Time
	EndAt   time.Time
}

// LogListResult 登录日志查询结果。
type LogListResult struct {
	Items    []model.SysLoginLog
	Total    int64
	Page     int
	PageSize int
}

// List 日志查询（数据来源即日志表本身，量级可控；索引已按 user/time/ip 三条覆盖）。
func (d *SysLoginLogDao) List(q LogListQuery) (*LogListResult, error) {
	page, size := normalizePage(q.Page, q.PageSize)
	db := d.db().Model(&model.SysLoginLog{})
	if q.UserId > 0 {
		db = db.Where("user_id = ?", q.UserId)
	}
	if q.Ip != "" {
		db = db.Where("ip = ?", q.Ip)
	}
	if q.Success != nil {
		db = db.Where("success = ?", *q.Success)
	}
	if !q.StartAt.IsZero() {
		db = db.Where("created_at >= ?", q.StartAt)
	}
	if !q.EndAt.IsZero() {
		db = db.Where("created_at <= ?", q.EndAt)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("sys_login_log.list.count: %w", err)
	}

	var items []model.SysLoginLog
	if err := db.Order("id DESC").Limit(size).Offset((page - 1) * size).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("sys_login_log.list.find: %w", err)
	}
	return &LogListResult{Items: items, Total: total, Page: page, PageSize: size}, nil
}
