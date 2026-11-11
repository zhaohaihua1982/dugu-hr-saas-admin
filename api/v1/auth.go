// 认证接口（docs §9 全部 auth 路由）。
package v1

import (
	"time"

	"github.com/gin-gonic/gin"

	appauth "dugu-hr-saas-admin/application/auth"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/i18n"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// 鉴权中间件写入 gin context 的 key（与 middleware.AuthGuard 约定一致）。
const (
	keyUID = "uid"
	keyJTI = "jti"
)

// GetCaptcha POST /auth/captcha。
func (p *AuthAPI) GetCaptcha(ctx *gin.Context) {
	resp, err := p.auth.GetCaptcha(ctx.Request.Context(), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, resp)
}

// SendSms POST /auth/sms/send。
func (p *AuthAPI) SendSms(ctx *gin.Context) {
	var req appauth.SendSmsReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.auth.SendSms(ctx.Request.Context(), &req, requestParams(ctx)); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	// 防枚举：无论手机号是否注册，成功口径一致（docs §5）；
	// 提示文案按请求语言（i18n.KeySmsSent）。
	l := i18n.FromAccept(ctx.GetHeader("Accept-Language"))
	if v := ctx.Query("lang"); v != "" {
		if parsed, ok := i18n.ParseLang(v); ok {
			l = parsed
		}
	}
	ctx.JSON(200, ctl.Response{
		Status: code.SUCCESS,
		Msg:    i18n.Msg(l, code.SUCCESS),
		Data:   i18n.Get(l, i18n.KeySmsSent),
	})
}

// LoginPassword POST /auth/login/password。
func (p *AuthAPI) LoginPassword(ctx *gin.Context) {
	var req appauth.LoginPasswordReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	// now 由接口层传给应用用例：时间属于边界，不藏在域内（可测性）
	resp, err := p.auth.LoginPassword(ctx.Request.Context(), &req, requestParams(ctx), time.Now())
	if err != nil {
		respondAuthError(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, resp)
}

// LoginSms POST /auth/login/sms。
func (p *AuthAPI) LoginSms(ctx *gin.Context) {
	var req appauth.LoginSmsReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	resp, err := p.auth.LoginSms(ctx.Request.Context(), &req, requestParams(ctx), time.Now())
	if err != nil {
		respondAuthError(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, resp)
}

// Logout POST /auth/logout（需登录态，白名单外由 AuthGuard 拦截）。
func (p *AuthAPI) Logout(ctx *gin.Context) {
	if err := p.auth.Logout(ctx.Request.Context(), ctx.GetInt64(keyUID), ctx.GetString(keyJTI)); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}

// Me GET /auth/me（需登录态）。密码时效三态：password_stale=已过期 /
// pwd_expiring=进入提前提醒窗口 / pwd_must_change=到期强制改密或首登强制改密
// （docs 登录配置设计 §2.4/§7）。
func (p *AuthAPI) Me(ctx *gin.Context) {
	user, expired, expiring, must, err := p.auth.Me(ctx.Request.Context(), ctx.GetInt64(keyUID), time.Now())
	if err != nil {
		respondAuthError(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, gin.H{
		"user":            user,
		"password_stale":  expired, // 兼容保留字段（语义=已过期）
		"pwd_expiring":    expiring,
		"pwd_must_change": must,
	})
}

// ChangePassword POST /auth/change-password —— 自助修改密码（需登录态）。
// 受 allow_self_change_pwd 门控（关闭 → 30053）；成功且 kick_on_pwd_change=1 时
// 全端秒踢（含当前端，前端应引导重新登录）。
func (p *AuthAPI) ChangePassword(ctx *gin.Context) {
	var req appauth.ChangePasswordReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.auth.ChangePassword(ctx.Request.Context(), ctx.GetInt64(keyUID), &req,
		requestParams(ctx), time.Now()); err != nil {
		respondAuthError(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}
