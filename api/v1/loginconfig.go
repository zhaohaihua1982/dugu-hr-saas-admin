// 登录配置管理接口（docs/系统管理-登录配置设计.md §5；v1.2 用户裁定：
// 读取端按端固定投影——GET -admin 写死管理后台配置，GET -tenant 预留留空；
// 原 GET 列表/GET :type 详情撤销）。
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

// parseConfigType 路径参数 :type → ConfigType（PUT/reset 用；非法 → 30048）。
func parseConfigType(ctx *gin.Context) (dloginconfig.ConfigType, error) {
	n, err := strconv.ParseInt(ctx.Param("type"), 10, 8)
	if err != nil {
		return 0, dloginconfig.ErrNotFound
	}
	t := dloginconfig.ConfigType(n)
	if !t.Valid() {
		return 0, dloginconfig.ErrNotFound
	}
	return t, nil
}

// GetAdmin GET /admin/login-configs-admin —— 管理后台登录配置（写死 config_type=1）。
// 投影：前端编辑页绑定字段 + version（docs 登录配置设计 §11.2-④）。
// v1.3 用户裁定：不设租户读取端——管理后台不拉租户数据；租户配置随租户登录
// 模块（/api/v1/tenant/* 体系）落地时再在彼侧定义接口。
func (p *LoginConfigAPI) GetAdmin(ctx *gin.Context) {
	v, err := p.svc.AdminSettings(ctx.Request.Context())
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
