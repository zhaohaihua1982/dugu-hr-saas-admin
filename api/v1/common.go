// Package v1 —— 接口层（DDD interfaces 层）：绑定请求 → 调应用层 → 统一响应。
// 仅薄壳：不编排业务、不触仓储，全部语义在 application 层。
package v1

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"dugu-hr-saas-admin/application"
	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// AuthAPI 认证接口。
type AuthAPI struct{ auth *application.AuthService }

// NewAuthAPI 构造。
func NewAuthAPI(auth *application.AuthService) *AuthAPI { return &AuthAPI{auth} }

// SysUserAPI 账号管理接口。
type SysUserAPI struct{ users *application.SysUserService }

// NewSysUserAPI 构造。
func NewSysUserAPI(users *application.SysUserService) *SysUserAPI { return &SysUserAPI{users} }

// LoginLogAPI 登录日志接口。
type LoginLogAPI struct{ logs *application.LoginLogService }

// NewLoginLogAPI 构造。
func NewLoginLogAPI(logs *application.LoginLogService) *LoginLogAPI { return &LoginLogAPI{logs} }

// ---------- 统一错误响应 ----------

// ErrorResponse 参数绑定/校验与其他业务错误的统一响应。
func ErrorResponse(ctx *gin.Context, err error) {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		ctl.RespError(ctx, err, code.InvalidParams, ve.Error())
		return
	}
	ctl.RespError(ctx, err, application.CodeOf(err), nil)
}

// respondAuthError 认证错误响应：data 附变长字段（remaining_seconds / captcha_required，docs §10）。
func respondAuthError(ctx *gin.Context, err error) {
	status := application.CodeOf(err)
	data := map[string]interface{}{}
	var le *authn.ErrLocked
	if errors.As(err, &le) {
		data["remaining_seconds"] = int64(le.Remaining / 1e9)
	}
	if status == code.ErrorCaptchaRequired {
		data["captcha_required"] = true
	}
	if len(data) == 0 {
		data = nil
	}
	ctx.AbortWithStatusJSON(200, ctl.Response{Status: status, Msg: code.GetMsg(status), Data: data})
}
