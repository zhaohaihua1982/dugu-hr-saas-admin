// Package token —— authn.TokenIssuer / TokenVerifier 端口实现（JWT HS256，docs §7）。
package token

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	jwtpkg "github.com/golang-jwt/jwt/v5"

	"dugu-hr-saas-admin/domain/authn"
)

const issuer = "dugu-hr-saas-admin"

// Service JWT 服务（HS256；密钥由配置注入 SAAS_JWT_SECRET）。
type Service struct{ secret []byte }

// New 构造（密钥 ≥16 字节由 bootstrap 校验）。
func New(secret string) *Service { return &Service{secret: []byte(secret)} }

var (
	_ authn.TokenIssuer   = (*Service)(nil)
	_ authn.TokenVerifier = (*Service)(nil)
)

// Issue 为 user 签发会话 token（exp=8h；jti 为 Redis 会话键）。
func (s *Service) Issue(_ context.Context, uid int64) (authn.Token, error) {
	jti := randJTI()
	claims := jwtpkg.RegisteredClaims{
		ID:        jti,
		Issuer:    issuer,
		IssuedAt:  jwtpkg.NewNumericDate(time.Now()),
		ExpiresAt: jwtpkg.NewNumericDate(time.Now().Add(authn.IssueTokenTTL)),
	}
	value, err := jwtpkg.NewWithClaims(jwtpkg.SigningMethodHS256, jwtClaims{
		UID:              uid,
		RegisteredClaims: claims,
	}).SignedString(s.secret)
	if err != nil {
		return authn.Token{}, fmt.Errorf("issue token: %w", err)
	}
	return authn.Token{Value: value, JTI: jti, TTL: authn.IssueTokenTTL}, nil
}

// Verify 解析+验签+过期校验；仅接受 HS256（防算法替换）。
func (s *Service) Verify(_ context.Context, raw string) (int64, string, error) {
	var c jwtClaims
	_, err := jwtpkg.ParseWithClaims(raw, &c, func(tk *jwtpkg.Token) (interface{}, error) {
		if _, ok := tk.Method.(*jwtpkg.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非法签名算法: %v", tk.Header["alg"])
		}
		return s.secret, nil
	}, jwtpkg.WithIssuer(issuer))
	if err != nil {
		if errors.Is(err, jwtpkg.ErrTokenExpired) {
			return 0, "", errors.New("token 已过期")
		}
		return 0, "", errors.New("token 无效")
	}
	return c.UID, c.RegisteredClaims.ID, nil
}

// jwtClaims 载荷：仅 user_id 与 jti（权限不进 token，docs §7）。
type jwtClaims struct {
	UID int64 `json:"uid"`
	jwtpkg.RegisteredClaims
}

// randJTI 16 字节随机 hex。
func randJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
