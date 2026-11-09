// 菜单管理接口（docs 三模块设计 §1/§1.1/§4 + 管理图 menu.jpg 转化）。
package v1

import (
	"strconv"

	"github.com/gin-gonic/gin"

	appmenu "dugu-hr-saas-admin/application/menus"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// MenuAPI 菜单管理接口。
type MenuAPI struct{ menus *appmenu.Service }

// NewMenuAPI 构造。
func NewMenuAPI(menus *appmenu.Service) *MenuAPI { return &MenuAPI{menus} }

// parseMenuID 解析路径参数 id；非法 → 30034（菜单参数不合法）。
func parseMenuID(ctx *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, appmenu.ErrBadMenuID
	}
	return id, nil
}

// Tree GET /admin/menus/tree?lang= —— 全量树（?lang= 决定展示名，回退链见应用层）。
func (p *MenuAPI) Tree(ctx *gin.Context) {
	nodes, err := p.menus.Tree(ctx.Request.Context(), ctx.Query("lang"))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nodes)
}

// Detail GET /admin/menus/:id —— 详情，含 names 全集（编辑页回填）。
func (p *MenuAPI) Detail(ctx *gin.Context) {
	id, err := parseMenuID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	node, err := p.menus.Detail(ctx.Request.Context(), id)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, node)
}

// Create POST /admin/menus。
func (p *MenuAPI) Create(ctx *gin.Context) {
	var req appmenu.UpsertMenuReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	node, err := p.menus.Create(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, node)
}

// Update PUT /admin/menus/:id。
func (p *MenuAPI) Update(ctx *gin.Context) {
	id, err := parseMenuID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	var req appmenu.UpdateMenuReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	node, err := p.menus.Update(ctx.Request.Context(), id, &req, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, node)
}

// Delete DELETE /admin/menus/:id。
func (p *MenuAPI) Delete(ctx *gin.Context) {
	id, err := parseMenuID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.menus.Delete(ctx.Request.Context(), id, ctx.GetInt64(keyUID), requestParams(ctx)); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}

// Sort POST /admin/menus/sort —— 批量保存排序（管理图“保存排序”；逐项独立）。
var _ = code.ErrorMenuNotFound // 保持 pkg/code 引用（排序逐项 code 由应用层填）

func (p *MenuAPI) Sort(ctx *gin.Context) {
	var req appmenu.SortReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	items := p.menus.Sort(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx))
	ctl.RespSuccess(ctx, items)
}
