// Package routes —— interfaces 层的路由装配（docs §9 接口清单）。
// Handler 依赖全部经构造注入（标准DDD：无包级全局服务单例）。
package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/api/v1"
	"dugu-hr-saas-admin/application"
	"dugu-hr-saas-admin/middleware"
)

// NewRouter 装配路由：/api/v1/admin/auth/* 登录体系；/api/v1/admin/* 管理面（需登录）。
// 与未来租户侧（/api/v1/tenant/auth/*）严格隔离、不共用登录代码（docs §2.1）。
// authMW 由 bootstrap 提供（JWT 校验 + Redis 会话 + 滑动续期）。
func NewRouter(svc *application.Services, authMW gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.Cors())

	r.GET("/healthz", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	authAPI := v1.NewAuthAPI(svc.Auth)
	userAPI := v1.NewSysUserAPI(svc.Users)
	logAPI := v1.NewLoginLogAPI(svc.Logs)

	v1Group := r.Group("api/v1")
	admin := v1Group.Group("admin")
	{
		auth := admin.Group("auth")
		{
			auth.POST("captcha", authAPI.GetCaptcha)
			auth.POST("sms/send", authAPI.SendSms)
			auth.POST("login/password", authAPI.LoginPassword)
			auth.POST("login/sms", authAPI.LoginSms)
		}

		authed := admin.Group("")
		authed.Use(authMW)
		{
			authed.POST("auth/logout", authAPI.Logout)
			authed.GET("auth/me", authAPI.Me)

			authed.GET("sys-users", userAPI.List)
			authed.POST("sys-users", userAPI.Create)
			authed.PUT("sys-users/:id", userAPI.Update)
			authed.DELETE("sys-users/:id", userAPI.Delete)
			authed.POST("sys-users/:id/unlock", userAPI.Unlock)
			authed.POST("sys-users/:id/reset-password", userAPI.ResetPassword)

			authed.GET("login-logs", logAPI.ListLogs)
		}
	}
	return r
}
