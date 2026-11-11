// 字典管理接口（docs/系统管理-字典(sys_dict)设计.md §3 接口清单）。
// 命名契约：取项用 type（code）而非 type_id；POST/PUT dict-items 用 typeCode（取舍 1/2）。
package v1

import (
	"strconv"

	"github.com/gin-gonic/gin"

	appdict "dugu-hr-saas-admin/application/dict"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/ctl"
)

// DictAPI 字典管理接口。
type DictAPI struct{ dict *appdict.Service }

// NewDictAPI 构造。
func NewDictAPI(dict *appdict.Service) *DictAPI { return &DictAPI{dict} }

// parseDictID 解析路径参数 id；非法 → 30046（字典参数不合法）。
func parseDictID(ctx *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(ctx.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, appdict.ErrBadDictID
	}
	return id, nil
}

// ListTypes GET /admin/dict-types?code=&status=&page=&page_size=
func (p *DictAPI) ListTypes(ctx *gin.Context) {
	var status *int
	if s := ctx.Query("status"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || (v != 0 && v != 1) {
			ctl.RespError(ctx, appdict.ErrBadDictID, code.ErrorDictBadParams, nil)
			return
		}
		status = &v
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(ctx.DefaultQuery("page_size", "10"))
	view, err := p.dict.ListTypes(ctx.Request.Context(), ctx.Query("code"), status, page, pageSize)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, view)
}

// CreateType POST /admin/dict-types。
func (p *DictAPI) CreateType(ctx *gin.Context) {
	var req appdict.UpsertTypeReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	view, err := p.dict.CreateType(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, view)
}

// UpdateType PUT /admin/dict-types/:id。
func (p *DictAPI) UpdateType(ctx *gin.Context) {
	id, err := parseDictID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	var req appdict.UpdateTypeReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	view, err := p.dict.UpdateType(ctx.Request.Context(), id, &req, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, view)
}

// DeleteType DELETE /admin/dict-types/:id（有项 → 30045）。
func (p *DictAPI) DeleteType(ctx *gin.Context) {
	id, err := parseDictID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.dict.DeleteType(ctx.Request.Context(), id, ctx.GetInt64(keyUID), requestParams(ctx)); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}

// SetTypeStatus POST /admin/dict-types/:id/status —— 独立启停（v1.4）。
func (p *DictAPI) SetTypeStatus(ctx *gin.Context) {
	id, err := parseDictID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	var req appdict.StatusReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.dict.SetTypeStatus(ctx.Request.Context(), id, &req, ctx.GetInt64(keyUID), requestParams(ctx)); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}

// SetItemStatus POST /admin/dict-items/:id/status —— 独立启停（v1.4）。
func (p *DictAPI) SetItemStatus(ctx *gin.Context) {
	id, err := parseDictID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	var req appdict.StatusReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.dict.SetItemStatus(ctx.Request.Context(), id, &req, ctx.GetInt64(keyUID), requestParams(ctx)); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}

// ListItems GET /admin/dict-items?type=import_type&lang=&withDisabled=0
// 响应 data 为 [{value,label}] 数组（前端下拉直接吃，docs §3）。
func (p *DictAPI) ListItems(ctx *gin.Context) {
	if ctx.Query("type") == "" {
		ctl.RespError(ctx, appdict.ErrBadDictID, code.ErrorDictBadParams, nil)
		return
	}
	var items interface{}
	var err error
	if ctx.Query("manage") == "1" {
		items, err = p.dict.ListItemsManage(ctx.Request.Context(), ctx.Query("type"),
			ctx.Query("lang"), ctx.Query("withDisabled") == "1")
	} else {
		items, err = p.dict.ListItems(ctx.Request.Context(), ctx.Query("type"),
			ctx.Query("lang"), ctx.Query("withDisabled") == "1")
	}
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, items)
}

// ItemDetail GET /admin/dict-items/:id（含 labels 全集，编辑页回填）。
func (p *DictAPI) ItemDetail(ctx *gin.Context) {
	id, err := parseDictID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	view, err := p.dict.ItemDetail(ctx.Request.Context(), id)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, view)
}

// CreateItem POST /admin/dict-items。
func (p *DictAPI) CreateItem(ctx *gin.Context) {
	var req appdict.UpsertItemReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	view, err := p.dict.CreateItem(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, view)
}

// UpdateItem PUT /admin/dict-items/:id（labels 只覆盖所传语言）。
func (p *DictAPI) UpdateItem(ctx *gin.Context) {
	id, err := parseDictID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	var req appdict.UpdateItemReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	view, err := p.dict.UpdateItem(ctx.Request.Context(), id, &req, ctx.GetInt64(keyUID), requestParams(ctx))
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, view)
}

// DeleteItem DELETE /admin/dict-items/:id（软删主表 + 译文行同事务）。
func (p *DictAPI) DeleteItem(ctx *gin.Context) {
	id, err := parseDictID(ctx)
	if err != nil {
		ErrorResponse(ctx, err)
		return
	}
	if err := p.dict.DeleteItem(ctx.Request.Context(), id, ctx.GetInt64(keyUID), requestParams(ctx)); err != nil {
		ErrorResponse(ctx, err)
		return
	}
	ctl.RespSuccess(ctx, nil)
}
