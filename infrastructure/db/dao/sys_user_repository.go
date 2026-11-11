// Package dao —— domain 仓储端口的 MySQL 实现（PO ↔ 领域实体互转在这一层完成）。
package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"dugu-hr-saas-admin/domain/loginlog"
	"dugu-hr-saas-admin/domain/sysuser"
	"dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/po"
)

// 编译期断言：两个 DAO 满足领域端口。
var (
	_ sysuser.Repository  = (*SysUserRepository)(nil)
	_ loginlog.Repository = (*LoginLogRepository)(nil)
)

// SysUserRepository sysuser.Repository 的实现。
type SysUserRepository struct{ d *db.DB }

// NewSysUserRepository 构造。
func NewSysUserRepository(d *db.DB) *SysUserRepository { return &SysUserRepository{d} }

func toDomainUser(p *po.SysUser) *sysuser.User {
	if p == nil {
		return nil
	}
	return &sysuser.User{
		ID:       p.ID,
		Username: p.Username,
		Name:     p.Name,
		Email:    p.Email,
		Status:   sysuser.Status(p.Status),
		Credential: sysuser.Credential{
			Hash:         p.PasswordHash,
			Algo:         p.PasswordAlgo,
			Salt:         p.Salt,
			PwdUpdatedAt: p.PwdUpdatedAt,
		},
		Mobile:         sysuser.Mobile{Hash: p.MobileHash, Enc: p.MobileEnc, Mask: p.MobileMask},
		FailCount:      p.FailCount,
		LockedUntil:    p.LockedUntil,
		LastLoginAt:    p.LastLoginAt,
		LastLoginIP:    p.LastLoginIP,
		PwdUpdatedAt:   p.PwdUpdatedAt,
		PwdForceChange: p.PwdForceChange,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

func toIPOStatus(s *sysuser.Status) *int {
	if s == nil {
		return nil
	}
	v := int(*s)
	return &v
}

// Create 新建聚合。
func (r *SysUserRepository) Create(ctx context.Context, u *sysuser.User) error {
	now := time.Now()
	p := &po.SysUser{
		ID:             u.ID,
		Username:       u.Username,
		PasswordHash:   u.Credential.Hash,
		PasswordAlgo:   u.Credential.Algo,
		Salt:           u.Credential.Salt,
		Name:           u.Name,
		MobileHash:     u.Mobile.Hash,
		MobileEnc:      u.Mobile.Enc,
		MobileMask:     u.Mobile.Mask,
		Email:          u.Email,
		Status:         int(u.Status),
		PwdUpdatedAt:   u.PwdUpdatedAt,
		PwdForceChange: u.PwdForceChange,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := r.d.WithContext(ctx).Create(p).Error; err != nil {
		if idx, dup := isDuplicate(err); dup {
			if idx == "uk_mobile" {
				return sysuser.ErrMobileTaken
			}
			return sysuser.ErrUsernameTaken
		}
		return fmt.Errorf("sysuser.create: %w", err)
	}
	u.CreatedAt, u.UpdatedAt = p.CreatedAt, p.UpdatedAt
	return nil
}

// ByID 按主键（软删过滤 deleted_at=0），未命中 (nil,nil)。
func (r *SysUserRepository) ByID(ctx context.Context, id int64) (*sysuser.User, error) {
	var p po.SysUser
	err := r.d.WithContext(ctx).First(&p, "id = ? AND deleted_at = 0", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sysuser.by_id: %w", err)
	}
	return toDomainUser(&p), nil
}

// ByUsername 精确匹配（登录解析①）。
func (r *SysUserRepository) ByUsername(ctx context.Context, username string) (*sysuser.User, error) {
	var p po.SysUser
	err := r.d.WithContext(ctx).First(&p, "username = ? AND deleted_at = 0", username).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sysuser.by_username: %w", err)
	}
	return toDomainUser(&p), nil
}

// ByMobileHash 手机哈希匹配（登录解析②）。
func (r *SysUserRepository) ByMobileHash(ctx context.Context, hash string) (*sysuser.User, error) {
	var p po.SysUser
	err := r.d.WithContext(ctx).First(&p, "mobile_hash = ? AND deleted_at = 0", hash).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sysuser.by_mobile_hash: %w", err)
	}
	return toDomainUser(&p), nil
}

// SaveProfile 更新 name/email/status。
func (r *SysUserRepository) SaveProfile(ctx context.Context, id int64, name, email string, status *sysuser.Status) error {
	sets := map[string]interface{}{}
	if name != "" {
		sets["name"] = name
	}
	if email != "" {
		sets["email"] = email
	}
	if status != nil {
		sets["status"] = *status
	}
	if len(sets) == 0 {
		return nil
	}
	res := r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(sets)
	if res.Error != nil {
		return fmt.Errorf("sysuser.save_profile: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return sysuser.ErrNotFound
	}
	return nil
}

// UpdateMobile 换绑手机号（三列一起改，docs §3.3①）。
func (r *SysUserRepository) UpdateMobile(ctx context.Context, id int64, m sysuser.Mobile) error {
	res := r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"mobile_hash": m.Hash,
			"mobile_enc":  m.Enc,
			"mobile_mask": m.Mask,
		})
	if res.Error != nil {
		if idx, dup := isDuplicate(res.Error); dup && idx == "uk_mobile" {
			return sysuser.ErrMobileTaken
		}
		return fmt.Errorf("sysuser.update_mobile: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return sysuser.ErrNotFound
	}
	return nil
}

// UpdatePassword 更新凭证（改密/重置/透明升级共用）。
// 注意：不清 pwd_force_change——透明升级（旧算法迁 bcrypt）也走此路径，不算改密；
// 标记的清除由改密链路显式调 SetPwdForceChange(false)。
func (r *SysUserRepository) UpdatePassword(ctx context.Context, id int64, c sysuser.Credential) error {
	res := r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"password_hash":  c.Hash,
			"password_algo":  c.Algo,
			"salt":           c.Salt,
			"pwd_updated_at": c.PwdUpdatedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("sysuser.update_password: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return sysuser.ErrNotFound
	}
	return nil
}

// SetPwdForceChange 首登强制改密标记置位/清除（登录配置 P1.5）。
func (r *SysUserRepository) SetPwdForceChange(ctx context.Context, id int64, on bool) error {
	res := r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Update("pwd_force_change", on)
	if res.Error != nil {
		return fmt.Errorf("sysuser.set_pwd_force_change: %w", res.Error)
	}
	return nil
}

// SaveLoginSuccess 登录成功：写最近登录、清零失败计数与锁定。
func (r *SysUserRepository) SaveLoginSuccess(ctx context.Context, id int64, ip string, at time.Time) error {
	return r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"last_login_at": at,
			"last_login_ip": ip,
			"fail_count":    0,
			"locked_until":  nil,
			"status":        gorm.Expr("IF(status = 3, 1, status)"),
		}).Error
}

// IncrFailures 失败计数 DB 兜底（原子自增）。
func (r *SysUserRepository) IncrFailures(ctx context.Context, id int64, now time.Time) (int, error) {
	res := r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Update("fail_count", gorm.Expr("fail_count + 1"))
	if res.Error != nil {
		return 0, fmt.Errorf("sysuser.incr_failures: %w", res.Error)
	}
	var p po.SysUser
	if err := r.d.WithContext(ctx).First(&p, "id = ?", id).Error; err != nil {
		return 0, fmt.Errorf("sysuser.incr_failures.reload: %w", err)
	}
	return p.FailCount, nil
}

// MarkLocked 达到阈值：写 locked_until 并置状态（docs §6）。
func (r *SysUserRepository) MarkLocked(ctx context.Context, id int64, until time.Time) error {
	return r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"locked_until": until,
			"status":       int(sysuser.StatusLocked),
		}).Error
}

// Unlock 解锁。
func (r *SysUserRepository) Unlock(ctx context.Context, id int64) error {
	res := r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"fail_count":   0,
			"locked_until": nil,
			"status":       int(sysuser.StatusActive),
		})
	if res.Error != nil {
		return fmt.Errorf("sysuser.unlock: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return sysuser.ErrNotFound
	}
	return nil
}

// SoftDelete 软删（deleted_at 写毫秒戳以参与唯一键，docs §3.1）。
func (r *SysUserRepository) SoftDelete(ctx context.Context, id int64, at time.Time) error {
	res := r.d.WithContext(ctx).Model(&po.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Update("deleted_at", at.UnixMilli())
	if res.Error != nil {
		return fmt.Errorf("sysuser.soft_delete: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return sysuser.ErrNotFound
	}
	return nil
}

// List 分页列表。
func (r *SysUserRepository) List(ctx context.Context, q sysuser.ListQuery) (*sysuser.ListResult, error) {
	page, size := normalizePage(q.Page, q.PageSize)
	d := r.d.WithContext(ctx).Model(&po.SysUser{}).Where("deleted_at = 0")
	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		d = d.Where("`name` LIKE ? OR username LIKE ?", like, like)
	}
	if q.Status != nil {
		d = d.Where("status = ?", *q.Status)
	}
	var total int64
	if err := d.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("sysuser.list.count: %w", err)
	}
	var items []po.SysUser
	if err := d.Order("id DESC").Limit(size).Offset((page - 1) * size).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("sysuser.list.find: %w", err)
	}
	out := make([]sysuser.User, 0, len(items))
	for i := range items {
		out = append(out, *toDomainUser(&items[i]))
	}
	return &sysuser.ListResult{Items: out, Total: total, Page: page, PageSize: size}, nil
}

func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
