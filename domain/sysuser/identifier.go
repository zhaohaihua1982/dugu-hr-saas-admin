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

// MaskIdentifier 输入标识脱敏（写入登录日志 identifier_mask，docs §3.2）。
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
