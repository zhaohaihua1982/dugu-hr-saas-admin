// Package v1 HTTP 接口层：绑定请求 → 调 service → 统一响应。
package v1

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"dugu-hr-saas-admin/pkg/e"
	"dugu-hr-saas-admin/pkg/utils/ctl"
	"dugu-hr-saas-admin/service"
)

// ErrorResponse 参数绑定/校验及其他业务错误的统一响应。
func ErrorResponse(ctx *gin.Context, err error) {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		ctl.RespError(ctx, err, e.InvalidParams, ve.Error())
		return
	}
	ctl.RespError(ctx, err, service.CodeOf(err), nil)
}

// respondAuthError 统一认证错误响应：data 附变长字段
// （remaining_seconds / captcha_required，docs §10）。
func respondAuthError(ctx *gin.Context, err error) {
	status := service.CodeOf(err)
	data := map[string]interface{}{}
	if locked, remaining := service.IsLocked(err); locked {
		data["remaining_seconds"] = remaining
	}
	if status == e.ErrorCaptchaRequired {
		data["captcha_required"] = true
	}
	if len(data) == 0 {
		data = nil
	}
	ctx.AbortWithStatusJSON(200, ctl.Response{
		Status: status,
		Msg:    e.GetMsg(status),
		Data:   data,
	})
}
