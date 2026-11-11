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
	// 安全修复：gin 默认信任所有代理（XFF 头可伪造）→ ClientIP 可捏造，
	// 全部 IP 维度限流（登录 20/min、验证码 10/min、撞库 50/h）会被绕过。
	// 显式不信任任何改写头：ClientIP 直取 TCP 对端地址。
	// TODO 部署在 nginx/SLB 后时，改用配置的白名单：r.SetTrustedProxies([]string{"10.0.0.0/8"})
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err)
	}
	r.Use(middleware.SecureHeaders())
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
	i18nAPI := v1.NewI18nAPI(svc.I18n)
	dictAPI := v1.NewDictAPI(svc.Dict)

	v1Group := r.Group("api/v1")
	admin := v1Group.Group("admin")
	{
		auth := admin.Group("auth")
		{
			// 图形码双开 GET/POST：智障地址栏只能发 GET（如 <img>/地址栏直访）；
			// 一次性存储/限流不受影响，签发是“免费资源”语义。
			auth.GET("captcha", authAPI.GetCaptcha)
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
		admin.POST("sys-users/batch-freeze", userAPI.BatchFreeze)
		admin.POST("sys-users/batch-unfreeze", userAPI.BatchUnfreeze)
		admin.POST("sys-users/:id/reset-password", userAPI.ResetPassword)
		admin.GET("login-logs", logAPI.ListLogs)

		// 菜单管理（docs 三模块设计 §4；gin 1.12 静态/参数路由可共存）：
		// tree 静态段优先于 :id 匹配，GET /menus/:id 拿不到 "tree"。
		menuAPI := v1.NewMenuAPI(svc.Menus)
		admin.GET("menus/tree", menuAPI.Tree)
		admin.GET("menus/:id", menuAPI.Detail)
		admin.POST("menus", menuAPI.Create)
		admin.PUT("menus/:id", menuAPI.Update)
		admin.DELETE("menus/:id", menuAPI.Delete)
		admin.POST("menus/sort", menuAPI.Sort)

		// 统一译文表管理（docs 基础设施-i18n设计 §7.5 翻译接口；本族 camelCase；
		// DELETE 不带 body：单条删除走 query，批量删除走 POST batch-delete）：
		admin.GET("i18n/meta", i18nAPI.Meta)
		admin.GET("i18n/texts", i18nAPI.Texts)
		admin.PUT("i18n/texts", i18nAPI.Save)
		admin.DELETE("i18n/texts", i18nAPI.DeleteText)
		admin.POST("i18n/texts/batch-delete", i18nAPI.BatchDelete)
		admin.GET("i18n/pending", i18nAPI.Pending)

		// 字典管理（docs 系统管理-字典(sys_dict)设计.md §3；登录即可见，不做权限过滤；
		// dict-items 静态/参数路由共存：GET /dict-items 下拉取值，GET /dict-items/:id 详情）
		admin.GET("dict-types", dictAPI.ListTypes)
		admin.POST("dict-types", dictAPI.CreateType)
		admin.PUT("dict-types/:id", dictAPI.UpdateType)
		admin.POST("dict-types/:id/status", dictAPI.SetTypeStatus)
		admin.DELETE("dict-types/:id", dictAPI.DeleteType)
		admin.GET("dict-items", dictAPI.ListItems)
		admin.GET("dict-items/:id", dictAPI.ItemDetail)
		admin.POST("dict-items", dictAPI.CreateItem)
		admin.PUT("dict-items/:id", dictAPI.UpdateItem)
		admin.POST("dict-items/:id/status", dictAPI.SetItemStatus)
		admin.DELETE("dict-items/:id", dictAPI.DeleteItem)
	}
	return r
}
