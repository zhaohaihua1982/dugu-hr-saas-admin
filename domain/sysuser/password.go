// 密码凭证值对象与哈希端口（docs §3.3②/§8.2）。
package sysuser

import (
	"errors"
	"time"
	"unicode"
)

// ErrWeakPassword 密码策略不变式被违反。
var ErrWeakPassword = errors.New("sysuser: weak password")

// Credential 密码凭证值对象：bcrypt 哈希自带盐，Salt 默认空（仅旧算法兼容）。
type Credential struct {
	Hash         string // algo=bcrypt 时为 bcrypt 串；algo=sha256 时为 hex(salt|pwd)
	Algo         string // "bcrypt" / "sha256" / ""（空按 bcrypt）
	Salt         string // 仅 sha256 旧算法使用
	PwdUpdatedAt *time.Time
}

// HasPassword 无密码（空 hash=仅短信登录，docs §3.1 表注释）。
func (c Credential) HasPassword() bool { return c.Hash != "" }

// Verify 校验凭证密码；upgrade 非空表示命中旧算法且校验成功，
// 应用层须透明升级为 bcrypt（docs §3.3②）。
func (c Credential) Verify(h PasswordHasher, pwd string) (ok bool, upgrade *Credential, err error) {
	return h.Verify(c, pwd)
}

// HashPassword 由哈希端口生成新凭证（新写入一律 bcrypt，Salt 留空，docs §3.3②）。
func HashPassword(h PasswordHasher, pwd string) (Credential, error) { return h.Hash(pwd) }

// ---------- 密码策略（docs §8.2：≥10 位且至少两类字符） ----------

// ValidatePassword 纯函数策略校验。
func ValidatePassword(pwd string) error {
	if len(pwd) < 10 {
		return ErrWeakPassword
	}
	var lower, upper, digit, other bool
	for _, r := range pwd {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	n := 0
	for _, b := range []bool{lower, upper, digit, other} {
		if b {
			n++
		}
	}
	if n < 2 {
		return ErrWeakPassword
	}
	return nil
}

// ---------- 端口 ----------

// PasswordHasher 密码哈希端口（infrastructure/crypto 实现）。
type PasswordHasher interface {
	// Hash 新凭证（bcrypt）。
	Hash(pwd string) (Credential, error)
	// Verify 校验；命中旧算法且成功即返回升级目标凭证（透明迁移）。
	Verify(current Credential, pwd string) (ok bool, upgrade *Credential, err error)
	// Decoy 防时序枚举陪跑：对“账号不存在”也执行一次等价指纹计算（docs §8.1）。
	Decoy(pwd string)
}
