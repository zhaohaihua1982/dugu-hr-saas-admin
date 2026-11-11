// Package sysuser —— sys_user 聚合。
//
// 标准DDD分层约束：本包**零外部框架依赖**（不 import gin/gorm/redis/config），
// 只依赖标准库。对外的基础设施能力（密码哈希、手机号加密、存储）一律以
// interface 端口定义在此，由 infrastructure 层实现（依赖倒置 DIP）。
package sysuser

import (
	"context"
	"errors"
	"time"
)

// ---------- 聚合内错误（领域语言） ----------

var (
	// ErrNotFound 聚合不存在。
	ErrNotFound = errors.New("sysuser: not found")
	// ErrUsernameTaken 账号唯一键冲突。
	ErrUsernameTaken = errors.New("sysuser: username taken")
	// ErrMobileTaken 手机号唯一键冲突。
	ErrMobileTaken = errors.New("sysuser: mobile taken")
	// ErrBadUsername username 违反不变式（含“形似手机号”约束，docs §3.3③）。
	ErrBadUsername = errors.New("sysuser: bad username")
	// ErrBadMobile 手机号违反不变式。
	ErrBadMobile = errors.New("sysuser: bad mobile")
)

// ---------- 状态（不变式，docs §6/§8.3） ----------

type Status int8

// StatusActive/StatusDisabled/StatusLocked 正常/停用/锁定。
const (
	StatusActive   Status = 1
	StatusDisabled Status = 2
	StatusLocked   Status = 3
)

// SuperAdminUsername 内置超管账号名（docs §8.3，表结构未设超管列，以固定名识别）。
const SuperAdminUsername = "admin"

// 安全策略阈值（docs §6/§8.2/§8.7）。
// 注意：登录配置（docs 登录配置设计）上线后，fail_threshold/lock_minutes/streak
// 的运行时真值来自 loginconfig.Provider 快照——这些常量保留为代码兜底默认值
// （同 §3.2 seed 值）与旧测试基线，管线不再直接读。
const (
	FailThreshold    = 5                // 连续错 5 次锁定
	LockDuration     = 15 * time.Minute // 锁 15 分钟；锁定期内尝试不重置计时
	StreakThreshold  = 3                // 密码登录连续失败 3 次后必带图形码
	PasswordStaleAge = 180 * 24 * time.Hour
)

// ---------- 聚合根 ----------

// User 管理账号聚合根。不变式：
//   - username 不变（登录解析锚点）且不得形似手机号（§3.3③）；
//   - 一个手机号只能是一个人（uk_mobile，§3.3①）；
//   - 停用/删除即全端会话失效（§7，由应用层编排，聚合持状态）。
type User struct {
	ID       int64
	Username string
	Name     string
	Email    string
	Status   Status

	Credential Credential // 密码凭证值对象
	Mobile     Mobile     // 手机号三列值对象

	FailCount   int
	LockedUntil *time.Time
	LastLoginAt *time.Time
	LastLoginIP string

	PwdUpdatedAt   *time.Time
	PwdForceChange bool // 首登强制改密标记（登录配置 P1.5）：登录响应 pwd_must_change，改密成功清除
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// IsDisabled 停用判定。
func (u *User) IsDisabled() bool { return u.Status == StatusDisabled }

// IsLocked 锁定判定（含时限）。now 由调用方传入——领域不读全局时钟，保证可测。
func (u *User) IsLocked(now time.Time) bool {
	return u.LockedUntil != nil && u.LockedUntil.After(now)
}

// LockedRemaining 锁定剩余时长；未锁定返回 0。
func (u *User) LockedRemaining(now time.Time) time.Duration {
	if !u.IsLocked(now) {
		return 0
	}
	return u.LockedUntil.Sub(now)
}

// IsSuper 内置超管识别（docs §8.3）。
func (u *User) IsSuper() bool { return u.Username == SuperAdminUsername }

// PasswordStale 密码超期提示（不强制，docs §8.2）。
// 登录配置上线后管线改用 PwdAging（按配置 maxAge/remind 计算）；本方法保留为
// 固定 180 天的旧口径（seed 校验等处复用）。
func (u *User) PasswordStale(now time.Time) bool {
	return u.PwdUpdatedAt != nil && now.Sub(*u.PwdUpdatedAt) > PasswordStaleAge
}

// PwdAging 按登录配置计算密码时效（docs 登录配置设计 §2.4/§7）：
//   - maxAgeDays=0 → 不过期（两个标志恒 false）；
//   - Expired：已过有效期（动作 1=提示、动作 2=登录响应 pwd_must_change）；
//   - Expiring：有效期内且剩余 ≤ remind_days（登录响应 pwd_expiring）。
//
// pwd_max_age_days 从 N 改为 0（取消过期）时无需清理存量标记：过期态是即时计算的
// （docs §8 边界：存量"已到期"标记随下次登录清除——本实现无落库标记，天然满足）。
func (u *User) PwdAging(now time.Time, maxAgeDays, remindDays int) (expired, expiring bool) {
	if maxAgeDays <= 0 || u.PwdUpdatedAt == nil {
		return false, false
	}
	deadline := u.PwdUpdatedAt.Add(time.Duration(maxAgeDays) * 24 * time.Hour)
	if now.After(deadline) {
		return true, false
	}
	return false, remindDays > 0 && now.After(deadline.Add(-time.Duration(remindDays)*24*time.Hour))
}

// ---------- 列表查询模型 ----------

// ListQuery 列表查询条件（从 1 起；模糊匹配姓名/账号；可选状态过滤）。
type ListQuery struct {
	Page     int
	PageSize int
	Keyword  string
	Status   *Status
}

// ListResult 列表查询结果。
type ListResult struct {
	Items    []User
	Total    int64
	Page     int
	PageSize int
}

// ---------- 仓储端口（由 infrastructure/db/dao 实现） ----------

// Repository sys_user 仓储端口。
type Repository interface {
	// Create 新建聚合；唯一键冲突返回 ErrUsernameTaken/ErrMobileTaken。
	Create(ctx context.Context, u *User) error
	// ByID 未命中返回 (nil, nil)。
	ByID(ctx context.Context, id int64) (*User, error)
	// ByUsername 精确匹配（唯一索引，登录解析①）。
	ByUsername(ctx context.Context, username string) (*User, error)
	// ByMobileHash 手机哈希匹配（唯一索引，登录解析②）。
	ByMobileHash(ctx context.Context, hash string) (*User, error)
	// SaveProfile 更新 name/email/status。
	SaveProfile(ctx context.Context, id int64, name, email string, status *Status) error
	// UpdateMobile 换绑手机号（三列一起改，§3.3①）；冲突返回 ErrMobileTaken。
	UpdateMobile(ctx context.Context, id int64, m Mobile) error
	// UpdatePassword 更新凭证（改密/重置/旧算法透明升级共用；不动 pwd_force_change
	// ——透明升级不算改密，标记由改密链路经 SetPwdForceChange 显式维护）。
	UpdatePassword(ctx context.Context, id int64, c Credential) error
	// SetPwdForceChange 首登强制改密标记置位/清除（登录配置 P1.5：
	// 管理员代建/重置密码且 pwd_force_first_change=1 时置位）。
	SetPwdForceChange(ctx context.Context, id int64, on bool) error
	// SaveLoginSuccess 登录成功：写最近登录、清零失败计数与锁定。
	SaveLoginSuccess(ctx context.Context, id int64, ip string, at time.Time) error
	// IncrFailures 失败计数（Redis 是加速，DB 兜底，docs §6）。
	IncrFailures(ctx context.Context, id int64, now time.Time) (count int, err error)
	// MarkLocked 写锁定截止时间并置状态（达到 FailThreshold 时）。
	MarkLocked(ctx context.Context, id int64, until time.Time) error
	// Unlock 解锁：清零计数、解冻、还原状态。
	Unlock(ctx context.Context, id int64) error
	// SoftDelete 软删（DELETE 语义：停用并保留审计数据）。
	SoftDelete(ctx context.Context, id int64, at time.Time) error
	// List 分页列表。
	List(ctx context.Context, q ListQuery) (*ListResult, error)
}

// UserTypeEnum 历史密码表 user_type 取值（docs 登录配置设计 §10 P1.5）。
const UserTypeEnumSysUser int8 = 1

// PasswordHistory 历史密码端口（infrastructure/db/dao 实现，sys_password_history 表）。
// 语义：改密/重置成功时 Append 旧 hash；新密码校验 RecentHashes 取最近 N 行
// 逐条 bcrypt 比对（docs 登录配置设计 §2.4）。
type PasswordHistory interface {
	Append(ctx context.Context, userType int8, userID int64, hash string) error
	RecentHashes(ctx context.Context, userType int8, userID int64, n int) ([]string, error)
}
