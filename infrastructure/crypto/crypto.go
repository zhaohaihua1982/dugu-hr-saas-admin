// Package crypto —— authn/sysuser 端口的基础设施实现：密码哈希与手机号三列。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"dugu-hr-saas-admin/domain/sysuser"
)

// 算法标识（与 docs §3.3② 一致）。
const (
	AlgoBcrypt = "bcrypt" // 新写入一律 bcrypt（cost 12，盐内含）；Salt 留空
	AlgoSHA256 = "sha256" // 旧算法兼容：hex(sha256(salt|pwd))
)

// BcryptCost docs §8.2：cost ≥ 12。
const BcryptCost = 12

// DecoyHash 假 hash 常量（docs §8.1 防时序枚举：账号不存在时陪跑一次等价计算）。
const DecoyHash = "$2a$12$6P.99L36bTWsqgBshH7uke6/UaEdWW1TyDoIS1l2liDwInkHnCrLq"

// PasswordHasher sysuser.PasswordHasher 实现。
type PasswordHasher struct{}

// NewPasswordHasher 构造。
func NewPasswordHasher() *PasswordHasher { return &PasswordHasher{} }

// 编译期断言：满足领域端口。
var _ sysuser.PasswordHasher = (*PasswordHasher)(nil)

// Hash 新凭证（bcrypt）。
func (h *PasswordHasher) Hash(pwd string) (sysuser.Credential, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pwd), BcryptCost)
	if err != nil {
		return sysuser.Credential{}, fmt.Errorf("bcrypt hash: %w", err)
	}
	return sysuser.Credential{Hash: string(b), Algo: AlgoBcrypt}, nil
}

// Verify 主路 bcrypt；sha256 旧算法命中且成功 → 返回升级目标（bcrypt 新凭证）。
func (h *PasswordHasher) Verify(current sysuser.Credential, pwd string) (bool, *sysuser.Credential, error) {
	algo := current.Algo
	if algo == "" {
		algo = AlgoBcrypt
	}
	switch algo {
	case AlgoBcrypt:
		return bcrypt.CompareHashAndPassword([]byte(current.Hash), []byte(pwd)) == nil, nil, nil
	case AlgoSHA256:
		if legacySHA256(current.Salt, pwd) == current.Hash {
			// 透明升级目标：bcrypt 重哈希；失败不阻断登录（应用层仅告警）
			up, err := h.Hash(pwd)
			if err != nil {
				return true, nil, nil
			}
			return true, &up, nil
		}
		return false, nil, nil
	default:
		// 未知算法按 bcrypt 校验（保守，安全位一致性不变）
		return bcrypt.CompareHashAndPassword([]byte(current.Hash), []byte(pwd)) == nil, nil, nil
	}
}

// Decoy 防时序陪跑（结果忽略，仅耗时）。
func (h *PasswordHasher) Decoy(pwd string) {
	_ = bcrypt.CompareHashAndPassword([]byte(DecoyHash), []byte(pwd))
}

// legacySHA256 旧算法指纹：sha256hex(salt|pwd)。
func legacySHA256(salt, pwd string) string {
	sum := sha256.Sum256([]byte(salt + "|" + pwd))
	return hex.EncodeToString(sum[:])
}

// ---------- 手机号三列（sysuser.MobileCrypto 实现） ----------

// MobileCrypto sysuser.MobileCrypto 实现（HMAC hash + AES-256-GCM enc/dec）。
type MobileCrypto struct {
	hmacKey []byte
	aesKey  []byte
}

// NewMobileCrypto 由配置密钥构造（bootstrap 注入）。
func NewMobileCrypto(hmacKey string, aesKeyHex string) (*MobileCrypto, error) {
	if len(hmacKey) < 16 {
		return nil, errors.New("mobileHmac ≥16 位")
	}
	key, err := hex.DecodeString(aesKeyHex)
	if err != nil || len(key) != 32 {
		return nil, errors.New("mobileAes 必须是 64 位 hex（32 字节 AES-256 键）")
	}
	return &MobileCrypto{hmacKey: []byte(hmacKey), aesKey: key}, nil
}

var _ sysuser.MobileCrypto = (*MobileCrypto)(nil)

// Hash HMAC-SHA256 归一化手机号 → 唯一索引入口。
func (c *MobileCrypto) Hash(normalized string) string {
	m := hmac.New(sha256.New, c.hmacKey)
	_, _ = m.Write([]byte(normalized))
	return hex.EncodeToString(m.Sum(nil))
}

// Encrypt AES-256-GCM（nonce 前置拼接，hex 输出）。
func (c *MobileCrypto) Encrypt(plain string) (string, error) {
	gcm, err := c.newGCM()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return hex.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Decrypt 还原明文（发短信/核对时）。
func (c *MobileCrypto) Decrypt(enc string) (string, error) {
	gcm, err := c.newGCM()
	if err != nil {
		return "", err
	}
	raw, err := hex.DecodeString(enc)
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

func (c *MobileCrypto) newGCM() (cipher.AEAD, error) {
	block, err := aes.NewCipher(c.aesKey)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
