// Package encryption 提供认证链路的密码与手机号三列（hash/enc/mask）处理。
// 规则出处：docs/SaaS管理后台-管理账号(sys_user)设计.md §3.3、§8。
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const (
	// AlgoBcrypt 新写入一律 bcrypt（cost 12，盐内含）；salt 列留空。
	AlgoBcrypt = "bcrypt"
	// AlgoSHA256 旧算法兼容：sha256hex(salt+"|"+password)，登录成功后透明升级 bcrypt。
	AlgoSHA256 = "sha256"

	// BcryptCost docs §8.2：cost ≥ 12。
	BcryptCost = 12
)

// DecoyHash 防时序枚举的固定假 hash（docs §8.1）。
// 账号不存在时用它陪跑一次 bcrypt 比较，抹平“不存在的账号响应快 80ms”的探测信号。
const DecoyHash = "$2a$12$6P.99L36bTWsqgBshH7uke6/UaEdWW1TyDoIS1l2liDwInkHnCrLq"

// HashPassword 新生成密码 hash：bcrypt，自带盐，salt 列留空。
func HashPassword(pwd string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pwd), BcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash: %w", err)
	}
	return string(b), nil
}

// CheckBcrypt 比较 bcrypt hash。
func CheckBcrypt(hash, pwd string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pwd)) == nil
}

// LegacySHA256 旧算法：sha256hex(salt+"|"+pwd)。
func LegacySHA256(salt, pwd string) string {
	h := sha256.Sum256([]byte(salt + "|" + pwd))
	return hex.EncodeToString(h[:])
}

// StrongPassword 密码策略：≥10 位且至少含两类字符（docs §8.2）。
func StrongPassword(pwd string) bool {
	if len(pwd) < 10 {
		return false
	}
	return classCount(pwd) >= 2
}

// classCount 统计密码包含的字符类别数：小写 / 大写 / 数字 / 其他。
func classCount(pwd string) int {
	var (
		hasLower, hasUpper, hasDigit, hasOther bool
	)
	for _, r := range pwd {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasOther = true
		}
	}
	n := 0
	for _, b := range []bool{hasLower, hasUpper, hasDigit, hasOther} {
		if b {
			n++
		}
	}
	return n
}

// ---------- 手机号三列 ----------

var mobileReg = regexp.MustCompile(`^1\d{10}$`)

// NormalizeMobile 归一化手机号：去 +86/86 前缀与空格、横线、括号。
// 返回 11 位标准格式（1 开头），不合法返回错误。
func NormalizeMobile(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '(', ')':
			return -1
		}
		return r
	}, s)

	if strings.HasPrefix(s, "+86") {
		s = s[3:]
	} else if strings.HasPrefix(s, "86") && len(s) == 13 {
		s = s[2:]
	}

	if !mobileReg.MatchString(s) {
		return "", fmt.Errorf("手机号格式不合法: %q", raw)
	}
	return s, nil
}

// MobileHMAC 手机号哈希（唯一索引查询入口 mobile_hash）。
// hmacKey 由配置注入（encryptSecret.mobileHmac）。
func MobileHMAC(normalized, hmacKey string) string {
	m := hmac.New(sha256.New, []byte(hmacKey))
	_, _ = m.Write([]byte(normalized))
	return hex.EncodeToString(m.Sum(nil))
}

// MobileEncrypt AES-256-GCM 加密（mobile_enc），key 为 64 位 hex 字符串。
func MobileEncrypt(plaintext, aesKeyHex string) (string, error) {
	key, err := hex.DecodeString(aesKeyHex)
	if err != nil || len(key) != 32 {
		return "", errors.New("mobileAes 必须是 64 位 hex（32 字节 AES-256 键）")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return hex.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

// MobileDecrypt 解密 mobile_enc。
func MobileDecrypt(cipherHex, aesKeyHex string) (string, error) {
	key, err := hex.DecodeString(aesKeyHex)
	if err != nil || len(key) != 32 {
		return "", errors.New("mobileAes 必须是 64 位 hex（32 字节 AES-256 键）")
	}
	raw, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("mobile_enc 长度不合法")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// MaskMobile 列表展示用脱敏：138****8000。
func MaskMobile(normalized string) string {
	if len(normalized) != 11 {
		return ""
	}
	return normalized[:3] + "****" + normalized[7:]
}

// LooksLikeMobile 判定“形似手机号”：剥离 +86/空格/横线/括号后形如 1+10 位数字。
// 仅用于登录解析的“要不要走手机号归一化”的分支判断，不做合法性结论。
func LooksLikeMobile(s string) bool {
	return mobileReg.MatchString(mobileLikeChars(s))
}

// ---------- 账号 ----------

var usernameDigitsOnly = regexp.MustCompile(`^\d{11}$`)

// mobileLikeChars 归一化字符：剥离 + 与空格、横线、括号，返回剩余字符。
func mobileLikeChars(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '+', ' ', '-', '(', ')':
			return -1
		}
		return r
	}, s)
}

// ValidUsername username 校验：非空、≤64 位；
// **禁止 11 位纯数字且禁止形似手机号**（docs §3.3③：含连字符的
// “138-0000-0000”式写法会让登录标识解析出现账号/手机歧义，源头禁止）。
func ValidUsername(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	if LooksLikeMobile(s) {
		return false
	}
	return !usernameDigitsOnly.MatchString(s)
}

// MaskInput 登录标识脱敏（写入日志表 identifier_mask）。
// 形似手机号按 138****8000（先剥符号）；其余按 ad***in 风格（首2+***+尾2，过短给 *）。
func MaskInput(identifier string) string {
	s := strings.TrimSpace(identifier)
	if LooksLikeMobile(s) {
		return MaskMobile(mobileLikeChars(s))
	}
	n := len(s)
	switch {
	case n <= 2:
		return strings.Repeat("*", n)
	case n <= 4:
		return s[:1] + strings.Repeat("*", n-1)
	default:
		return s[:2] + "***" + s[n-2:]
	}
}
