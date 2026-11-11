// loginconfig.Repository 的 gorm 实现（docs 登录配置设计 §3/§5）。
// 配置行 ≤ 2 行、不可删；Save 走乐观锁（WHERE id AND version），冲突由实现复查行后归一。
package dao

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	"dugu-hr-saas-admin/domain/loginconfig"
	"dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/po"
)

// LoginConfigRepository 登录配置仓储。
type LoginConfigRepository struct{ db *db.DB }

// NewLoginConfigRepository 构造。
func NewLoginConfigRepository(d *db.DB) *LoginConfigRepository { return &LoginConfigRepository{db: d} }

var _ loginconfig.Repository = (*LoginConfigRepository)(nil)

// ByType 按 (config_type, scope_id=0) 取唯一行；未命中 (nil, nil)。
func (r *LoginConfigRepository) ByType(ctx context.Context, t loginconfig.ConfigType) (*loginconfig.Config, error) {
	var p po.SysLoginConfig
	err := r.db.WithContext(ctx).Where("config_type = ? AND scope_id = 0", int8(t)).First(&p).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return toLoginConfig(&p), nil
}

// Save 全量保存（乐观锁）。影响 0 行时复查：行消失 → ErrNotFound，版本不符 → ErrVersionConflict。
func (r *LoginConfigRepository) Save(ctx context.Context, c *loginconfig.Config) (int, error) {
	p, err := toLoginConfigPO(c)
	if err != nil {
		return 0, err
	}
	newVer := c.Version + 1
	p.Version = newVer
	extrasVal := interface{}(nil) // 空 → NULL（JSON 列不接受空串）
	if p.Extras != "" {
		extrasVal = p.Extras
	}
	res := r.db.WithContext(ctx).Model(&po.SysLoginConfig{}).
		Where("id = ? AND config_type = ? AND version = ?", c.ID, int8(c.ConfigType), c.Version).
		Updates(map[string]interface{}{
			"login_mask": p.LoginMask, "fail_threshold": p.FailThreshold,
			"lock_minutes": p.LockMinutes, "unlock_mode": p.UnlockMode,
			"captcha_after_fails": p.CaptchaAfterFails, "ip_fail_limit": p.IPFailLimit,
			"ip_rate_limit": p.IPRateLimit, "access_token_ttl": p.AccessTokenTTL,
			"refresh_token_ttl": p.RefreshTokenTTL, "session_hard_ttl": p.SessionHardTTL,
			"session_max_concurrent": p.SessionMaxConcurrent, "kick_on_pwd_change": p.KickOnPwdChange,
			"remember_days": p.RememberDays, "pwd_level": p.PwdLevel,
			"pwd_min_length": p.PwdMinLength, "pwd_min_classes": p.PwdMinClasses,
			"pwd_forbid_username": p.PwdForbidUsername, "pwd_max_age_days": p.PwdMaxAgeDays,
			"pwd_expire_action": p.PwdExpireAction, "pwd_remind_days": p.PwdRemindDays,
			"pwd_history_count": p.PwdHistoryCount, "pwd_force_first_change": p.PwdForceFirstChange,
			"allow_self_change_pwd": p.AllowSelfChangePwd, "extras": extrasVal,
			"version": newVer, "updated_by": c.UpdatedBy,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 { // 复查：行消失 or 版本冲突
		var cnt int64
		if err := r.db.WithContext(ctx).Model(&po.SysLoginConfig{}).
			Where("id = ?", c.ID).Count(&cnt).Error; err != nil {
			return 0, err
		}
		if cnt == 0 {
			return 0, loginconfig.ErrNotFound
		}
		return 0, loginconfig.ErrVersionConflict
	}
	return newVer, nil
}

// Create 建行（reset 时行缺失的兜底路径；ID 用类型固定 seed ID，幂等冲突忽略）。
func (r *LoginConfigRepository) Create(ctx context.Context, c *loginconfig.Config) error {
	p, err := toLoginConfigPO(c)
	if err != nil {
		return err
	}
	if c.ID == 0 {
		p.ID = c.ConfigType.SeedID()
	}
	return r.db.WithContext(ctx).Create(p).Error
}

// ---------- 聚合 ↔ PO ----------

func toLoginConfig(p *po.SysLoginConfig) *loginconfig.Config {
	c := &loginconfig.Config{
		ID: p.ID, ConfigType: loginconfig.ConfigType(p.ConfigType), ScopeID: p.ScopeID,
		LoginMask: p.LoginMask, FailThreshold: int(p.FailThreshold), LockMinutes: int(p.LockMinutes),
		UnlockMode: p.UnlockMode, CaptchaAfterFails: int(p.CaptchaAfterFails),
		IPFailLimit: int(p.IPFailLimit), IPRateLimit: int(p.IPRateLimit),
		AccessTokenTTL: int(p.AccessTokenTTL), RefreshTokenTTL: int(p.RefreshTokenTTL),
		SessionHardTTL: int(p.SessionHardTTL), SessionMaxConcurrent: int(p.SessionMaxConcurrent),
		KickOnPwdChange: p.KickOnPwdChange, RememberDays: int(p.RememberDays),
		PwdLevel: p.PwdLevel, PwdMinLength: int(p.PwdMinLength), PwdMinClasses: int(p.PwdMinClasses),
		PwdForbidUsername: p.PwdForbidUsername, PwdMaxAgeDays: int(p.PwdMaxAgeDays),
		PwdExpireAction: p.PwdExpireAction, PwdRemindDays: int(p.PwdRemindDays),
		PwdHistoryCount: int(p.PwdHistoryCount), PwdForceFirstChange: p.PwdForceFirstChange,
		AllowSelfChangePwd: p.AllowSelfChangePwd, Version: p.Version, UpdatedBy: p.UpdatedBy,
		CreatedAtStr: p.CreatedAt.Format(time.DateTime), UpdatedAtStr: p.UpdatedAt.Format(time.DateTime),
	}
	if p.Extras != "" {
		c.Extras = json.RawMessage(p.Extras)
	}
	return c
}

func toLoginConfigPO(c *loginconfig.Config) (*po.SysLoginConfig, error) {
	extras := ""
	if len(c.Extras) > 0 {
		var m map[string]interface{}
		if err := json.Unmarshal(c.Extras, &m); err != nil || len(m) == 0 {
			extras = "" // 空对象/null 存 NULL
		} else {
			b, mErr := json.Marshal(m)
			if mErr != nil {
				return nil, mErr
			}
			extras = string(b)
		}
	}
	return &po.SysLoginConfig{
		ID: c.ID, ConfigType: int8(c.ConfigType), ScopeID: c.ScopeID, LoginMask: c.LoginMask,
		FailThreshold: int16(c.FailThreshold), LockMinutes: int16(c.LockMinutes), UnlockMode: c.UnlockMode,
		CaptchaAfterFails: int16(c.CaptchaAfterFails), IPFailLimit: int16(c.IPFailLimit),
		IPRateLimit: int16(c.IPRateLimit), AccessTokenTTL: int32(c.AccessTokenTTL),
		RefreshTokenTTL: int32(c.RefreshTokenTTL), SessionHardTTL: int32(c.SessionHardTTL),
		SessionMaxConcurrent: int16(c.SessionMaxConcurrent), KickOnPwdChange: c.KickOnPwdChange,
		RememberDays: int16(c.RememberDays), PwdLevel: c.PwdLevel,
		PwdMinLength: int16(c.PwdMinLength), PwdMinClasses: int16(c.PwdMinClasses),
		PwdForbidUsername: c.PwdForbidUsername, PwdMaxAgeDays: int32(c.PwdMaxAgeDays),
		PwdExpireAction: c.PwdExpireAction, PwdRemindDays: int16(c.PwdRemindDays),
		PwdHistoryCount: int16(c.PwdHistoryCount), PwdForceFirstChange: c.PwdForceFirstChange,
		AllowSelfChangePwd: c.AllowSelfChangePwd, Extras: extras,
		Version: c.Version, UpdatedBy: c.UpdatedBy,
	}, nil
}

// ---------- 历史密码仓储（docs 登录配置设计 §10 P1.5） ----------

// PasswordHistoryRepository sysuser.PasswordHistory 端口实现。
type PasswordHistoryRepository struct {
	db  *db.DB
	ids interface{ New() int64 }
}

// NewPasswordHistoryRepository 构造（ids：应用侧雪花 ID）。
func NewPasswordHistoryRepository(d *db.DB, ids interface{ New() int64 }) *PasswordHistoryRepository {
	return &PasswordHistoryRepository{db: d, ids: ids}
}

// Append 追加旧 hash（只增不改）。
func (r *PasswordHistoryRepository) Append(ctx context.Context, userType int8, userID int64, hash string) error {
	row := &po.SysPasswordHistory{ID: r.ids.New(), UserType: userType, UserID: userID, PasswordHash: hash}
	if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
		return fmt.Errorf("写历史密码: %w", err)
	}
	return nil
}

// RecentHashes 最近 N 行旧 hash（created_at 倒序；N≤0 返回空）。
func (r *PasswordHistoryRepository) RecentHashes(ctx context.Context, userType int8, userID int64, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	var rows []po.SysPasswordHistory
	if err := r.db.WithContext(ctx).
		Where("user_type = ? AND user_id = ?", userType, userID).
		Order("created_at DESC").Limit(n).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.PasswordHash)
	}
	return out, nil
}
