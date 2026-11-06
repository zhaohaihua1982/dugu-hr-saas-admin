// Package routes 路由注册（docs §9 接口清单）。
package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"

	api "dugu-hr-saas-admin/api/v1"
	"dugu-hr-saas-admin/middleware"
)

// NewRouter 路由配置：/api/v1/admin/auth/* 登录体系；/api/v1/admin/* 管理面（需登录）。
// 与未来租户侧（/api/v1/tenant/auth/*）严格隔离，不共用任何登录代码（docs §2.1）。
func NewRouter() *gin.Engine {
	r := gin.Default()
	r.Use(middleware.Cors())

	r.GET("/healthz", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	v1 := r.Group("api/v1")
	admin := v1.Group("admin")
	{
		auth := admin.Group("auth")
		{
			auth.POST("captcha", api.GetCaptchaHandler())
			auth.POST("sms/send", api.SendSmsHandler())
			auth.POST("login/password", api.LoginPasswordHandler())
			auth.POST("login/sms", api.LoginSmsHandler())
		}

		// 需要登录态的接口
		authed := admin.Group("")
		authed.Use(middleware.JwtAuth())
		{
			authed.POST("auth/logout", api.LogoutHandler())
			authed.GET("auth/me", api.MeHandler())

			// 账号管理
			authed.GET("sys-users", api.ListSysUsersHandler())
			authed.POST("sys-users", api.CreateSysUserHandler())
			authed.PUT("sys-users/:id", api.UpdateSysUserHandler())
			authed.DELETE("sys-users/:id", api.DeleteSysUserHandler())
			authed.POST("sys-users/:id/unlock", api.UnlockSysUserHandler())
			authed.POST("sys-users/:id/reset-password", api.ResetPasswordHandler())

			// 登录日志
			authed.GET("login-logs", api.ListLoginLogsHandler())
		}
	}
	return r
}
