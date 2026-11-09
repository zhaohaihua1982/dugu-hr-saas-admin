// Package po —— 表对象（PO）：字段与 config/sql/*.sql 中的 DDL 一一对应，仅本包使用。
package po

import "time"

// SysUser sys_user 表对象（docs §3.1）。软删参与唯一键：deleted_at 默认 0，删除时写毫秒戳。
type SysUser struct {
	ID           int64      `gorm:"primaryKey;column:id"` // 雪花ID，应用侧生成
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
	LastLoginIP  string     `gorm:"column:last_login_ip"`
	PwdUpdatedAt *time.Time `gorm:"column:pwd_updated_at"`
	DeletedAt    int64      `gorm:"column:deleted_at"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at"`
}

// TableName 表名。
func (SysUser) TableName() string { return "sys_user" }

// SysLoginLog sys_login_log 表对象（docs §3.2；v1.2：identifier 账号原样/手机脱敏）。
type SysLoginLog struct {
	ID             int64     `gorm:"primaryKey;column:id"`
	UserID         int64     `gorm:"column:user_id"`
	IdentifierMask string    `gorm:"column:identifier_mask"`
	LoginType      string    `gorm:"column:login_type"`
	IdentVia       string    `gorm:"column:ident_via"`
	Success        bool      `gorm:"column:success"`
	FailReason     string    `gorm:"column:fail_reason"`
	IP             string    `gorm:"column:ip"`
	UA             string    `gorm:"column:ua"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

// TableName 表名。
func (SysLoginLog) TableName() string { return "sys_login_log" }
