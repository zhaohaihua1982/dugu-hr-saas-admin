// Package model 数据库表对象（PO），字段与 config/sql/*.sql 中的 DDL 一一对应。
package model

import "time"

// 用户状态
const (
	UserStatusActive   = 1
	UserStatusDisabled = 2
	UserStatusLocked   = 3
)

// SysUser 管理账号表（docs §3.1）。软删参与唯一键：deleted_at 默认 0，删除时写毫秒戳。
type SysUser struct {
	Id           int64      `gorm:"primaryKey;column:id"` // 雪花ID，应用侧生成
	Username     string     `gorm:"column:username"`
	PasswordHash string     `gorm:"column:password_hash"`
	PasswordAlgo string     `gorm:"column:password_algo"`
	Salt         string     `gorm:"column:salt"`
	Name         string     `gorm:"column:name"`
	MobileHash   string     `gorm:"column:mobile_hash"`
	MobileEnc    string     `gorm:"column:mobile_enc"`
	MobileMask   string     `gorm:"column:mobile_mask"`
	Email        string     `gorm:"column:email"`
	Status       int        `gorm:"column:status"`
	FailCount    int        `gorm:"column:fail_count"`
	LockedUntil  *time.Time `gorm:"column:locked_until"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at"`
	LastLoginIp  string     `gorm:"column:last_login_ip"`
	PwdUpdatedAt *time.Time `gorm:"column:pwd_updated_at"`
	DeletedAt    int64      `gorm:"column:deleted_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

func (SysUser) TableName() string { return "sys_user" }

// IsDisabled 停用判定。
func (u *SysUser) IsDisabled() bool { return u.Status == UserStatusDisabled }

// IsLockedAt 锁定判定（锁定到未来某时刻）。
func (u *SysUser) IsLockedAt(now time.Time) bool {
	return u.LockedUntil != nil && u.LockedUntil.After(now)
}

// SysLoginLog 登录日志表（docs §3.2）。写事务外、存脱敏标识、fail_reason 固定枚举。
type SysLoginLog struct {
	Id             int64     `gorm:"primaryKey;column:id"`
	UserId         int64     `gorm:"column:user_id"`
	IdentifierMask string    `gorm:"column:identifier_mask"`
	LoginType      string    `gorm:"column:login_type"`
	IdentVia       string    `gorm:"column:ident_via"`
	Success        bool      `gorm:"column:success"`
	FailReason     string    `gorm:"column:fail_reason"`
	Ip             string    `gorm:"column:ip"`
	Ua             string    `gorm:"column:ua"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (SysLoginLog) TableName() string { return "sys_login_log" }
