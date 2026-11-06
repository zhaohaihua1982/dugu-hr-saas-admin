// 会话鉴权中间件（JWT + Redis 会话 + 滑动续期，docs §7）。
// 依赖注入：token 校验与会话存储均为 domain/authn 端口（由 bootstrap 装配）。
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// JwtAuth 会话鉴权中间件：
//  1. 解析 Authorization: Bearer <JWT>（token 只带 uid/jti，权限不进 token）；
//  2. Redis 会话键必须有效且归属一致（登出/秒踢即时生效）；
//  3. 每次请求滑动续期 8h（24h 硬上限由 sess:abs 控制）。
func JwtAuth(verifier authn.TokenVerifier, sessions authn.SessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
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
			c.AbortWithStatusJSON(http.StatusOK, ctl.Response{
				Status: code.ErrorDatabase, Msg: code.GetMsg(code.ErrorDatabase),
			})
			return
		}
		if sessUID != uid { // 防串号：JWT 载荷与会话键归属不一致视为失效
			authAbort(c)
			return
		}

		// 滑动续期（有效性已验证；此步失败不阻断请求）
		_ = sessions.Slide(c.Request.Context(), jti)

		c.Set("uid", uid)
		c.Set("jti", jti)
		c.Next()
	}
}
