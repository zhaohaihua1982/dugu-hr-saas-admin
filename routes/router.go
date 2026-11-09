// Package routes —— interfaces 层的路由装配（docs §9 接口清单）。
//
// 鉴权策略：全局统一过滤 + 显式白名单——
//   - 公共接口（healthz、登录入口、图形码、发短信）写入白名单；
//   - 其余接口一律要求登录态（安全默认 deny：新增接口忘放行配置也会被拦下）。
package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "dugu-hr-saas-admin/api/v1"
	"dugu-hr-saas-admin/application"
	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/middleware"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// PublicPaths 白名单：未登录即可访问。
//
//	/healthz 探针；/auth/captcha 图形码；/auth/sms/send 发短信（自身有图形码+配额防护）；
//	/auth/login/* 登录入口。logout/me 不在白名单（需要登录态）。
//
// 注意白名单不要放得太宽：auth/* 会连带放行 logout，故逐条列举登录入口。
var PublicPaths = []string{
	"/healthz",
	"/api/v1/admin/auth/captcha",
	"/api/v1/admin/auth/sms/send",
	"/api/v1/admin/auth/login/password",
	"/api/v1/admin/auth/login/sms",
}

// NewRouter 装配路由：全局 AuthGuard（未登录统一过滤，白名单放行）。
// 与未来租户侧（/api/v1/tenant/auth/*）严格隔离、不共用登录代码（docs §2.1）。
func NewRouter(svc *application.Services, verifier authn.TokenVerifier, sessions authn.SessionStore) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.Cors())
	// 全局统一鉴权过滤：白名单外的所有请求必须携带有效会话
	r.Use(middleware.AuthGuard(verifier, sessions, middleware.NewWhitelist(PublicPaths...)))

	// 未注册路由/方法不匹配的兜底：返回 JSON 提示而非 gin 默认的纯文本 404，
	// 帮助前端把“路径拼错/ METHOD 用错（如 GET 访问 POST 接口）”一次定位。
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": code.ErrorNotFoundNoRoute,
			"msg":    ctl.MsgOf(c, code.ErrorNotFoundNoRoute),
			"data":   gin.H{"method": c.Request.Method, "path": c.Request.URL.Path},
		})
	})
	r.NoMethod(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": code.ErrorNotFoundNoRoute,
			"msg":    ctl.MsgOf(c, code.ErrorNotFoundNoRoute),
			"data":   gin.H{"method": c.Request.Method, "path": c.Request.URL.Path},
		})
	})

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
			auth.POST("logout", authAPI.Logout)
			auth.GET("me", authAPI.Me)
		}

		// 管理面：全局 AuthGuard 已要求登录，此处不再重复挂鉴权
		admin.GET("sys-users", userAPI.List)
		admin.POST("sys-users", userAPI.Create)
		admin.PUT("sys-users/:id", userAPI.Update)
		admin.DELETE("sys-users/:id", userAPI.Delete)
		admin.POST("sys-users/:id/unlock", userAPI.Unlock)
		admin.POST("sys-users/:id/reset-password", userAPI.ResetPassword)
		admin.GET("login-logs", logAPI.ListLogs)
	}
	return r
}
