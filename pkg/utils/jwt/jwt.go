// Package jwt 会话令牌（docs §7）：JWT 只带 user_id 与 jti，权限不进 token。
// 每次请求同时校验 Redis 会话键（配合 repository/cache 的 sess:{jti}），便于秒踢。
package jwt

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"dugu-hr-saas-admin/consts"
)

const issuer = "dugu-hr-saas-admin"

// Claims 自定义载荷：仅 user_id 与 jti。
type Claims struct {
	UID int64 `json:"uid"`
	jwt.RegisteredClaims
}

// Token 会话令牌工具。secret 由配置注入，进程内共享一个实例。
type Token struct {
	secret []byte
}

func New(secret string) *Token {
	return &Token{secret: []byte(secret)}
}

// GenerateToken 为 user 签发会话 token（exp = 8h，配合 Redis 键滑动续期）。
// 返回 JWT 串与 jti（jti 又作为 Redis 会话键，供登出/秒踢）。
func (t *Token) GenerateToken(uid int64) (token, jti string, err error) {
	jti = randJTI()
	claims := Claims{
		UID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(consts.IdleSessionTTL)),
		},
	}
	token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", "", fmt.Errorf("签发 token: %w", err)
	}
	return token, jti, nil
}

// ParseToken 校验并解析 token。仅接受 HS256（防算法替换攻击），
// 过期校验由 jwt/v5 内建完成。
func (t *Token) ParseToken(token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(tk *jwt.Token) (interface{}, error) {
		if _, ok := tk.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非法签名算法: %v", tk.Header["alg"])
		}
		return t.secret, nil
	}, jwt.WithIssuer(issuer))
	if err != nil {
		// 过期和签名错误都统一为失效
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errors.New("token 已过期")
		}
		return nil, errors.New("token 无效")
	}
	return claims, nil
}

// randJTI 生成 16 字节随机 hex 作为会话 ID。
func randJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
