// 登录标识解析领域服务（docs §3.3③/§4）。
// 歧义消除在源头（username 禁形似手机号），因此两条解析规则不会同时命中：
//
//	① username 精确匹配（唯一索引）
//	② 形似手机号 → 归一化 → mobile_hash 匹配
//	③ 都未命中 → nil（应用层走“不存在”分支并陪跑）
package sysuser

import "context"

// Via 登录命中的标证类型（写日志 ident_via 使用）。
type Via string

// ViaUsername / ViaMobile 命中种类。
const (
	ViaUsername Via = "USERNAME"
	ViaMobile   Via = "MOBILE"
)

// Resolved 解析结果。
type Resolved struct {
	User *User
	Via  Via
}

// ResolveIdentifier 按 docs §3.3③ 的两条规则解析登录标识。
// finder 由调用方给实（通常直接传 Repository，它同时满足本接口）。
func ResolveIdentifier(ctx context.Context, raw string, finder interface {
	ByUsername(ctx context.Context, username string) (*User, error)
	ByMobileHash(ctx context.Context, hash string) (*User, error)
}, md MobileCrypto) (*Resolved, error) {
	// ① username 精确匹配
	u, err := finder.ByUsername(ctx, raw)
	if err != nil {
		return nil, err
	}
	if u != nil {
		return &Resolved{User: u, Via: ViaUsername}, nil
	}

	// ② 形似手机号 → 归一化 → mobile_hash 匹配
	if LooksLikeMobile(raw) {
		n, nerr := NormalizeMobile(raw)
		if nerr == nil {
			u2, err2 := finder.ByMobileHash(ctx, md.Hash(n))
			if err2 != nil {
				return nil, err2
			}
			if u2 != nil {
				return &Resolved{User: u2, Via: ViaMobile}, nil
			}
		}
	}
	return nil, nil // ③ 都未命中
}

// PlausibleIdentifier 登录入口对标识的快筛：非空、≤64（表列宽）、无控制字符。
// 不做格式正向断言（账号规则各异）——显然非法的请求认真管，其余交管线。
func PlausibleIdentifier(raw string) bool {
	if raw == "" || len(raw) > 64 {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < 0x20 || raw[i] == 0x7f {
			return false
		}
	}
	return true
}

// LogIdentifier 登录日志 identifier 列的落库值（docs §3.2 v1.2 调整）：
//   - 账号 → **原样**（账号非 PII，原样才能精确回查与审计关联）；
//   - 形似手机号 → 归一化后 138****8000（手机号是 PII，绝不落原文）。
//
// 调用方输入即登录请求里的原始 identifier。
func LogIdentifier(raw string) string {
	if LooksLikeMobile(raw) {
		if n, err := NormalizeMobile(raw); err == nil {
			return MaskMobile(n)
		}
	}
	return raw
}

// MaskIdentifier 保留用于“标识面包粉”类键（防爆破计数器的 key 分桶），
// 登录日志落库不再使用（v1.2 起账号原样）。
// 形似手机号按 138****8000；其余按 ad***in 风格（首2+***+尾2，过短给 *）。
func MaskIdentifier(raw string) string {
	if LooksLikeMobile(raw) {
		n, err := NormalizeMobile(raw)
		if err == nil {
			return MaskMobile(n)
		}
	}
	n := len(raw)
	switch {
	case n <= 2:
		return repeat("*", n)
	case n <= 4:
		return raw[:1] + repeat("*", n-1)
	default:
		return raw[:2] + "***" + raw[n-2:]
	}
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
