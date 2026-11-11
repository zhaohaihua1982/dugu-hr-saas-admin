// 登录配置管理接口（docs/系统管理-登录配置设计.md §5）。
package v1

import (
	"strconv"

	"github.com/gin-gonic/gin"

	apploginconfig "dugu-hr-saas-admin/application/loginconfig"
	dloginconfig "dugu-hr-saas-admin/domain/loginconfig"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// LoginConfigAPI 登录配置接口。
type LoginConfigAPI struct{ svc *apploginconfig.Service }

// NewLoginConfigAPI 构造。
func NewLoginConfigAPI(svc *apploginconfig.Service) *LoginConfigAPI { return &LoginConfigAPI{svc} }

// parseConfigType 路径参数 :type → ConfigType（非法 → 30048）。
func parseConfigType(ctx *gin.Context) (dloginconfig.ConfigType, error) {
	n, err := strconv.ParseInt(ctx.Param("type"), 10, 16)
	if err != nil {
		return 0, dloginconfig.ErrNotFound
	}
	t := dloginconfig.ConfigType(n)
	if !t.Valid() {
		return 0, dloginconfig.ErrNotFound
	}
	return t, nil
}

// List GET /admin/login-configs —— 两种类型各一行（含兜底横幅 fallback）。
func (p *LoginConfigAPI) List(ctx *gin.Context) {
	list, err := p.svc.List(ctx.Request.Context())
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, list)
}

// Get GET /admin/login-configs/:type —— 单条详情。
func (p *LoginConfigAPI) Get(ctx *gin.Context) {
	t, err := parseConfigType(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	v, err := p.svc.Get(ctx.Request.Context(), t)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, v)
}

// Update PUT /admin/login-configs/:type —— 全量更新（body 带 version 乐观锁）。
// 成功返回新版本号；缓存立即失效（§5 约定 3）。
func (p *LoginConfigAPI) Update(ctx *gin.Context) {
	t, err := parseConfigType(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	var req apploginconfig.UpdateReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	resp, err := p.svc.Update(ctx.Request.Context(), t, &req, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, resp)
}

// Reset POST /admin/login-configs/:type/reset —— 恢复该类型默认值（前端须二次确认）。
func (p *LoginConfigAPI) Reset(ctx *gin.Context) {
	t, err := parseConfigType(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	resp, err := p.svc.Reset(ctx.Request.Context(), t, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, resp)
}
