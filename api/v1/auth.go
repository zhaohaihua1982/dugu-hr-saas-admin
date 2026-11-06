package v1

import (
	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/pkg/utils/ctl"
	"dugu-hr-saas-admin/service"
	"dugu-hr-saas-admin/types"
)

// GetCaptchaHandler 获取图形验证码（docs §9）。
func GetCaptchaHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		resp, err := service.GetAuthSrv().GetCaptcha()
		if err != nil {
			ctl.RespError(ctx, err, service.CodeOf(err), nil)
			return
		}
		ctl.RespSuccess(ctx, resp)
	}
}

// SendSmsHandler 发送登录短信（需图形码，docs §9）。
func SendSmsHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req types.SendSmsReq
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		if err := service.GetAuthSrv().SendSms(ctx, &req, ctx.ClientIP()); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		// 防枚举：无论手机号是否注册，成功口径一致
		ctl.RespSuccess(ctx, "已发送")
	}
}

// LoginPasswordHandler 账号或手机 + 密码（docs §9）。
func LoginPasswordHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req types.LoginPasswordReq
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		resp, err := service.GetAuthSrv().LoginPassword(ctx, &req, ctx.ClientIP(), ctx.GetHeader("User-Agent"))
		if err != nil {
			respondAuthError(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, resp)
	}
}

// LoginSmsHandler 手机 + 短信码（docs §9）。
func LoginSmsHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req types.LoginSmsReq
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		resp, err := service.GetAuthSrv().LoginSms(ctx, &req, ctx.ClientIP(), ctx.GetHeader("User-Agent"))
		if err != nil {
			respondAuthError(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, resp)
	}
}

// LogoutHandler 登出当前会话（需要登录态）。
func LogoutHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uid := ctx.GetInt64(middlewareKeyUID)
		jti := ctx.GetString(middlewareKeyJTI)
		if err := service.GetAuthSrv().Logout(uid, jti); err != nil {
			ctl.RespError(ctx, err, service.CodeOf(err), nil)
			return
		}
		ctl.RespSuccess(ctx, nil)
	}
}

// MeHandler 当前用户信息（需要登录态）。
func MeHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		uid := ctx.GetInt64(middlewareKeyUID)
		user, stale, err := service.GetAuthSrv().Me(ctx, uid)
		if err != nil {
			respondAuthError(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, gin.H{
			"user":           user,
			"password_stale": stale,
		})
	}
}
