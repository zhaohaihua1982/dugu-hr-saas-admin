package v1

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/pkg/utils/ctl"
	"dugu-hr-saas-admin/service"
	"dugu-hr-saas-admin/types"
)

// 鉴权中间件写入 gin context 的 key（middleware.JwtAuth 与之保持一致）。
const (
	middlewareKeyUID = "uid"
	middlewareKeyJTI = "jti"
)

// CreateSysUserHandler POST /admin/sys-users。
func CreateSysUserHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req types.CreateSysUserReq
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		user, err := service.GetSysUserSrv().Create(ctx, &req, ctx.GetInt64(middlewareKeyUID), ctx.ClientIP())
		if err != nil {
			ErrorResponse(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, user)
	}
}

// ListSysUsersHandler GET /admin/sys-users。
func ListSysUsersHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req types.SysUserListReq
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		resp, err := service.GetSysUserSrv().List(ctx, &req)
		if err != nil {
			ErrorResponse(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, resp)
	}
}

// UpdateSysUserHandler PUT /admin/sys-users/{id}。
func UpdateSysUserHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id, err := parseID(ctx)
		if err != nil {
			ErrorResponse(ctx, err)
			return
		}
		var req types.UpdateSysUserReq
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		user, err := service.GetSysUserSrv().Update(ctx, id, &req, ctx.GetInt64(middlewareKeyUID), ctx.ClientIP())
		if err != nil {
			ErrorResponse(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, user)
	}
}

// DeleteSysUserHandler DELETE /admin/sys-users/{id}（停用软删）。
func DeleteSysUserHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id, err := parseID(ctx)
		if err != nil {
			ErrorResponse(ctx, err)
			return
		}
		if err := service.GetSysUserSrv().Delete(ctx, id, ctx.GetInt64(middlewareKeyUID), ctx.ClientIP()); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, nil)
	}
}

// UnlockSysUserHandler POST /admin/sys-users/{id}/unlock。
func UnlockSysUserHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id, err := parseID(ctx)
		if err != nil {
			ErrorResponse(ctx, err)
			return
		}
		if err := service.GetSysUserSrv().Unlock(ctx, id, ctx.GetInt64(middlewareKeyUID), ctx.ClientIP()); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, nil)
	}
}

// ResetPasswordHandler POST /admin/sys-users/{id}/reset-password（强审计）。
func ResetPasswordHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		id, err := parseID(ctx)
		if err != nil {
			ErrorResponse(ctx, err)
			return
		}
		var req types.ResetPasswordReq
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		if err := service.GetSysUserSrv().ResetPassword(ctx, id, &req, ctx.GetInt64(middlewareKeyUID), ctx.ClientIP()); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, nil)
	}
}

// parseID 解析路径参数 id。
func parseID(ctx *gin.Context) (int64, error) {
	return strconv.ParseInt(ctx.Param("id"), 10, 64)
}
