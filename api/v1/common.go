// Package v1 —— 接口层（DDD interfaces 层）：绑定请求 → 调应用层 → 统一响应。
// 仅薄壳：不编排业务、不触仓储，全部语义在 application 层（auth/sysuser/loginlog 子包）。
package v1

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"dugu-hr-saas-admin/application"
	appauth "dugu-hr-saas-admin/application/auth"
	apploginlog "dugu-hr-saas-admin/application/loginlog"
	appsysuser "dugu-hr-saas-admin/application/sysuser"
	"dugu-hr-saas-admin/application/types"
	"dugu-hr-saas-admin/domain/authn"
	ddict "dugu-hr-saas-admin/domain/dict"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// AuthAPI 认证接口。
type AuthAPI struct{ auth *appauth.Service }

// NewAuthAPI 构造。
func NewAuthAPI(auth *appauth.Service) *AuthAPI { return &AuthAPI{auth} }

// SysUserAPI 账号管理接口。
type SysUserAPI struct{ users *appsysuser.Service }

// NewSysUserAPI 构造。
func NewSysUserAPI(users *appsysuser.Service) *SysUserAPI { return &SysUserAPI{users} }

// LoginLogAPI 登录日志接口。
type LoginLogAPI struct{ logs *apploginlog.Service }

// NewLoginLogAPI 构造。
func NewLoginLogAPI(logs *apploginlog.Service) *LoginLogAPI { return &LoginLogAPI{logs} }

// ---------- 统一错误响应 ----------

// ErrorResponse 参数绑定/校验与其他业务错误的统一响应。
func ErrorResponse(ctx *gin.Context, err error) {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		fields := make([]string, 0, len(ve))
		for _, e := range ve {
			fields = append(fields, e.Field())
		}
		ctl.RespError(ctx, err, code.InvalidParams, fields)
		return
	}
	// 字典 30046 带违规字段提示（v1.5）：BadField 包装保持 errors.Is 语义，
	// data=[字段名] 让前端/联调一眼定位是哪个参数不合法。
	var bf *ddict.BadField
	if errors.As(err, &bf) {
		ctl.RespError(ctx, err, application.CodeOf(err), []string{bf.Field})
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
		data["remaining_seconds"] = int64(le.Remaining / time.Second)
	}
	if status == code.ErrorCaptchaRequired {
		data["captcha_required"] = true
	}
	if len(data) == 0 {
		data = nil
	}
	ctx.AbortWithStatusJSON(200, ctl.Response{Status: status, Msg: ctl.MsgOf(ctx, status), Data: data})
}

// requestParams 提取 IP/UA 传给应用层。
func requestParams(ctx *gin.Context) types.RequestParams {
	return types.RequestParams{IP: ctx.ClientIP(), UA: ctx.GetHeader("User-Agent")}
}
