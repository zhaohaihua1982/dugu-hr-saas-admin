// 会话鉴权：全局统一过滤 + 白名单放行。
// 未登录可访问的 URL 显式列白，其余一律要求登录态——安全默认 deny，
// 新增接口忘挂保护即被拦（配合 routes 使用）。
package middleware

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/loginconfig"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// AuthGuard 全局鉴权过滤：
//  1. 命中白名单（exact 或 通配前缀）→ 放行；
//  2. 未命中 → Authorization: Bearer JWT + Redis 会话有效 + 归属一致；
//  3. 认证请求滑动续期（TTL=登录配置 access_token_ttl，硬上限键不变，docs 登录配置设计 §7）。
func AuthGuard(verifier authn.TokenVerifier, sessions authn.SessionStore, cfg loginconfig.Provider, wl *Whitelist) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 白名单命中（healthz、登录入口等未登录接口）直接放行
		if wl != nil && wl.Match(c.Request.URL.Path) {
			c.Next()
			return
		}

		parts := strings.SplitN(c.GetHeader("Authorization"), " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			authAbort(c)
			return
		}

		uid, jti, err := verifier.Verify(context.Background(), parts[1])
		if err != nil {
			authAbort(c)
			return
		}

		// 会话有效性（登录态在 Redis，删键即踢）
		sessUID, serr := sessions.Get(c.Request.Context(), jti)
		switch {
		case errors.Is(serr, authn.ErrSessionNotFound):
			authAbort(c)
			return
		case serr != nil:
			c.AbortWithStatusJSON(200, ctl.Response{
				Status: code.ErrorDatabase, Msg: ctl.MsgOf(c, code.ErrorDatabase), Data: nil,
			})
			return
		}
		// 防串号：JWT 载荷与会话键归属不一致视为失效
		if sessUID != uid {
			authAbort(c)
			return
		}

		// 滑动续期（有效性已验证；此步失败不阻断请求）。
		// TTL 取登录配置快照（进程内存缓存 30s，每请求成本可忽略）；
		// 读取失败按 0 处理 → Expire 负值会被 Redis 拒绝，故仅在 TTL>0 时执行。
		if snap, pErr := cfg.Snapshot(c.Request.Context(), loginconfig.TypeAdmin); pErr == nil {
			if ttl := time.Duration(snap.Config.AccessTokenTTL) * time.Second; ttl > 0 {
				_ = sessions.Slide(c.Request.Context(), jti, ttl)
			}
		} else {
			_ = sessions.Slide(c.Request.Context(), jti, authn.IssueTokenTTL)
		}

		c.Set("uid", uid)
		c.Set("jti", jti)
		c.Next()
	}
}
