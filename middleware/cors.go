// Package middleware —— interfaces 层中间件：CORS、会话鉴权。
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// Cors 跨域（管理后台前端本地开发必需）。
func Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// authAbort 鉴权失败标准响应（登录态失效，业务码 30001）。
// 文案按请求语言协商（?lang=/Accept-Language，pkg/utils/ctl）。
func authAbort(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusOK, ctl.Response{
		Status: code.ErrorAuthCheckTokenFail,
		Msg:    ctl.MsgOf(c, code.ErrorAuthCheckTokenFail),
		Data:   nil,
	})
}

// SecureHeaders 基础安全响应头（API 服务最小集：
// 防 MIME 嗅探、防点劫持嵌入、浏览器缓存策略）。
func SecureHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}
