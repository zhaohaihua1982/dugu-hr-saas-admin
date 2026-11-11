// 数据文案管理接口（docs《基础设施-数据文案多语言(i18n)设计.md》§7 译文管理 CRUD）。
// 本族 JSON 字段名为 camelCase（§7.5 契约示例），与其余模块 snake_case 的差异是文档明示的取舍。
package v1

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	appi18n "dugu-hr-saas-admin/application/i18n"
	di18n "dugu-hr-saas-admin/domain/i18n"
	"dugu-hr-saas-admin/pkg/code"
	pkgi18n "dugu-hr-saas-admin/pkg/i18n"
	"dugu-hr-saas-admin/pkg/utils/ctl"
	"dugu-hr-saas-admin/pkg/utils/log"
)

// I18nAPI 统一译文管理接口。
type I18nAPI struct{ i18n *appi18n.Service }

// NewI18nAPI 构造。
func NewI18nAPI(i18n *appi18n.Service) *I18nAPI { return &I18nAPI{i18n} }

// langOf 与 ctl.lang 相同优先级：?lang= > Accept-Language > 配置默认。
func langOf(ctx *gin.Context) pkgi18n.Lang {
	if v := ctx.Query("lang"); v != "" {
		if l, ok := pkgi18n.ParseLang(v); ok {
			return l
		}
	}
	return pkgi18n.FromAccept(ctx.GetHeader("Accept-Language"))
}

// i18nResp 统一响应：成功/默认语言专用文案（§7.4"默认语言请通过资源编辑修改"）/ 通用错误。
func i18nResp(ctx *gin.Context, data interface{}, err error) {
	if err == nil {
		ctl.RespSuccess(ctx, data)
		return
	}
	if errors.Is(err, di18n.ErrDefaultLang) {
		log.Errorf("业务错误: status=%d err=%v", code.InvalidParams, err)
		ctx.JSON(200, ctl.Response{
			Status: code.InvalidParams,
			Msg:    pkgi18n.Get(langOf(ctx), "i18n.defaultLang"),
			Data:   []string{"texts"},
		})
		return
	}
	ErrorResponse(ctx, err)
}

// Meta GET /admin/i18n/meta?resType= —— 字段白名单 + 已注册语言 + 默认语言（前端渲染 tab 用）。
func (p *I18nAPI) Meta(ctx *gin.Context) {
	view, err := p.i18n.Meta(ctx.Request.Context(), ctx.Query("resType"))
	i18nResp(ctx, view, err)
}

// Texts GET /admin/i18n/texts —— 查译文（resId 可选，docs §7.5 v1.4/v1.6/v1.7）：
//   - resType + resId → 单资源全部译文（详情/表单回填，原语义）；
//   - resType[&field=&page=&page_size=] → 翻译表行直读列表（非聚合；
//     正常分页 page/page_size，回 total；与 sys-users 列表同一惯例）。
func (p *I18nAPI) Texts(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.Query("page"))
	pageSize, _ := strconv.Atoi(ctx.Query("page_size"))
	view, err := p.i18n.Texts(ctx.Request.Context(),
		ctx.Query("resType"), ctx.Query("field"), ctx.Query("resId"), page, pageSize)
	i18nResp(ctx, view, err)
}

// Save PUT /admin/i18n/texts —— 批量保存（只覆盖所传语言；空串 = 删除该语言）。
func (p *I18nAPI) Save(ctx *gin.Context) {
	var req appi18n.PutTextsReq
	if bindErr := ctx.ShouldBindJSON(&req); bindErr != nil {
		ErrorResponse(ctx, bindErr)
		return
	}
	view, err := p.i18n.PutTexts(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx))
	i18nResp(ctx, view, err)
}

// DeleteText DELETE /admin/i18n/texts?resType=&resId=&field=&lang= —— 单条删除（幂等，
// 删不存在 = 成功；默认语言无译文行 → i18n.defaultLang 文案 400）。
func (p *I18nAPI) DeleteText(ctx *gin.Context) {
	err := p.i18n.DeleteText(ctx.Request.Context(),
		ctx.Query("resType"), ctx.Query("resId"), ctx.Query("field"), ctx.Query("lang"),
		ctx.GetInt64(keyUID), requestParams(ctx))
	i18nResp(ctx, map[string]interface{}{"deleted": true}, err)
}

// BatchDelete POST /admin/i18n/texts/batch-delete —— 批量删除（≤200，单事务；
// 所传语言传空串 = 删除该语言（与 Repo.Save 语义一致）。
func (p *I18nAPI) BatchDelete(ctx *gin.Context) {
	var req appi18n.BatchDeleteReq
	if bindErr := ctx.ShouldBindJSON(&req); bindErr != nil {
		ErrorResponse(ctx, bindErr)
		return
	}
	out, err := p.i18n.DeleteBatch(ctx.Request.Context(), &req, ctx.GetInt64(keyUID), requestParams(ctx))
	i18nResp(ctx, out, err)
}

// Pending GET /admin/i18n/pending?resType=&lang=&afterId=&limit= —— 缺译清单（游标，无 total）。
func (p *I18nAPI) Pending(ctx *gin.Context) {
	afterID, _ := strconv.ParseInt(ctx.Query("afterId"), 10, 64)
	limit, _ := strconv.Atoi(ctx.Query("limit"))
	view, err := p.i18n.Pending(ctx.Request.Context(), ctx.Query("resType"), ctx.Query("lang"), afterID, limit)
	i18nResp(ctx, view, err)
}
