// 账号管理与登录日志接口（docs §9）。
package v1

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	apploginlog "dugu-hr-saas-admin/application/loginlog"
	appsysuser "dugu-hr-saas-admin/application/sysuser"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// CreateSysUser POST /admin/sys-users。
func (p *SysUserAPI) Create(ctx *gin.Context) {
	var req appsysuser.CreateSysUserReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	user, err := p.users.Create(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, user)
}

// List GET /admin/sys-users。
func (p *SysUserAPI) List(ctx *gin.Context) {
	var req appsysuser.SysUserListReq
	if err := ctx.ShouldBindQuery(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	resp, err := p.users.List(ctx.Request.Context(), &req, ctx.GetInt64(keyUID))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, resp)
}

// Update PUT /admin/sys-users/:id。
func (p *SysUserAPI) Update(ctx *gin.Context) {
	id, err := parseID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	var req appsysuser.UpdateSysUserReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	user, err := p.users.Update(ctx.Request.Context(), id, &req, ctx.GetInt64(keyUID), requestParams(ctx), time.Now())
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, user)
}

// Delete DELETE /admin/sys-users/:id（停用软删）。
func (p *SysUserAPI) Delete(ctx *gin.Context) {
	id, err := parseID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.users.Delete(ctx.Request.Context(), id, ctx.GetInt64(keyUID), requestParams(ctx), time.Now()); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}

// BatchFreeze POST /admin/sys-users/batch-freeze（逐项独立，含自我/超管/未找到明细）。
func (p *SysUserAPI) BatchFreeze(ctx *gin.Context) {
	var req appsysuser.BatchFreezeReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	resp := p.users.FreezeUsers(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx), time.Now())
	ctl.RespSuccess(ctx, resp)
}

// BatchUnfreeze POST /admin/sys-users/batch-unfreeze。
func (p *SysUserAPI) BatchUnfreeze(ctx *gin.Context) {
	var req appsysuser.BatchUnlockReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	resp := p.users.UnfreezeUsers(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx))
	ctl.RespSuccess(ctx, resp)
}

// ResetPassword POST /admin/sys-users/:id/reset-password（强审计）。
func (p *SysUserAPI) ResetPassword(ctx *gin.Context) {
	id, err := parseID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	var req appsysuser.ResetPasswordReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.users.ResetPassword(ctx.Request.Context(), id, &req, ctx.GetInt64(keyUID), requestParams(ctx), time.Now()); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}

// ListLogs GET /admin/login-logs。
func (p *LoginLogAPI) ListLogs(ctx *gin.Context) {
	var req apploginlog.ListLoginLogsReq
	if err := ctx.ShouldBindQuery(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	resp, err := p.logs.List(ctx.Request.Context(), &req)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, resp)
}

// parseID 解析路径参数 id。
func parseID(ctx *gin.Context) (int64, error) {
	return strconv.ParseInt(ctx.Param("id"), 10, 64)
}
