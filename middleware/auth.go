package middleware

import (
	"errors"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/pkg/e"
	"dugu-hr-saas-admin/pkg/utils/jwt"
	"dugu-hr-saas-admin/repository/cache"
)

var (
	tokenOnce sync.Once
	tokenSrc  *jwt.Token
)

// tokenService 惰性初始化：配置在 main 里 loading() 之后可用，
// 中间件首个请求前完成初始化（进程内单例，无竞态前提：请求前一定初始化）。
func tokenService() *jwt.Token {
	tokenOnce.Do(func() {
		tokenSrc = jwt.New(conf.Config.EncryptSecret.JwtSecret)
	})
	return tokenSrc
}

// JwtAuth 会话鉴权中间件（docs §7）：
//  1. 解析 Authorization: Bearer <JWT>（只带 uid/jti，权限不进 token）；
//  2. Redis 会话键必须有效（登出/秒踢即时生效）；
//  3. 每次请求滑动续期 8h（硬上限 24h 由 sess:abs:{jti} 控制）。
func JwtAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			authAbort(c)
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			authAbort(c)
			return
		}

		claims, err := tokenService().ParseToken(parts[1])
		if err != nil {
			authAbort(c)
			return
		}

		// 会话有效性（登录态在 Redis，删键即踢）
		sessUID, serr := cache.GetSessionUID(claims.ID)
		switch {
		case errors.Is(serr, cache.ErrSessionNotFound):
			authAbort(c)
			return
		case serr != nil:
			c.AbortWithStatusJSON(200, gin.H{
				"status": e.ErrorDatabase,
				"msg":    e.GetMsg(e.ErrorDatabase),
				"data":   nil,
			})
			return
		}
		// 防串号：会话键归属与 JWT 载荷不一致视为失效
		if sessUID != claims.UID {
			authAbort(c)
			return
		}

		// 滑动续期（有效性已在 GetSessionUID 校验过；此步失败不阻断请求）
		_ = cache.SlideSession(claims.ID)

		c.Set("uid", claims.UID)
		c.Set("jti", claims.ID)
		c.Next()
	}
}
