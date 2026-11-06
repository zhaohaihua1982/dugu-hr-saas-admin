// 手机号值对象与加密端口（docs §3.3①：查询走 hash、使用走密文、展示走脱敏）。
package sysuser

import (
	"fmt"
	"regexp"
	"strings"
)

var mobileReg = regexp.MustCompile(`^1\d{10}$`)

// Mobile 手机号三列值对象：hash（唯一索引查询入口）、enc（AES-GCM 密文）、mask（展示）。
// 聚合内**不携带明文**；明文仅在发短信等用例里由 MobileCrypto 端口临时解密。
type Mobile struct {
	Hash string
	Enc  string
	Mask string
}

// Validate 值对象不变式：三列必须齐备（docs §3.3①）。
func (m Mobile) Validate() error {
	if m.Hash == "" || m.Enc == "" || m.Mask == "" {
		return fmt.Errorf("sysuser: mobile 三列不完整")
	}
	return nil
}

// MobileCrypto 手机号加密端口（infrastructure/crypto 实现，持有密钥）。
type MobileCrypto interface {
	// Hash HMAC-SHA256(归一化手机号)。
	Hash(normalized string) string
	// Encrypt AES-GCM 加密。
	Encrypt(plain string) (string, error)
	// Decrypt 密文还原明文（发短信/核对时）。
	Decrypt(enc string) (string, error)
}

// BindMobile 由明文构造值对象：normalize → hash → encrypt → mask。
// crypto 为 nil 时只做归一化与脱敏（无端口场景）。
func BindMobile(plain string, crypto MobileCrypto) (Mobile, error) {
	n, err := NormalizeMobile(plain)
	if err != nil {
		return Mobile{}, ErrBadMobile
	}
	m := Mobile{Mask: MaskMobile(n)}
	if crypto != nil {
		m.Hash = crypto.Hash(n)
		enc, e := crypto.Encrypt(n)
		if e != nil {
			return Mobile{}, e
		}
		m.Enc = enc
	}
	return m, nil
}

// ---------- 纯函数（无端口依赖） ----------

// NormalizeMobile 归一化：去 +86/86 前缀与空格、横线、括号；返回 11 位标准格式。
// 违反不变式返回 ErrBadMobile（docs §3.3）。
func NormalizeMobile(raw string) (string, error) {
	s := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '(', ')', '+':
			return -1
		}
		return r
	}, strings.TrimSpace(raw))

	if strings.HasPrefix(s, "86") && len(s) == 13 {
		s = s[2:]
	}
	if !mobileReg.MatchString(s) {
		return "", ErrBadMobile
	}
	return s, nil
}

// LooksLikeMobile 是否“形似手机号”（剥符号后形如 1+10 位），供解析②分支判断。
func LooksLikeMobile(raw string) bool {
	n, err := NormalizeMobile(raw)
	return err == nil && mobileReg.MatchString(n)
}

// MaskMobile 展示脱敏：138****8000。
func MaskMobile(normalized string) string {
	if len(normalized) != 11 {
		return ""
	}
	return normalized[:3] + "****" + normalized[7:]
}
