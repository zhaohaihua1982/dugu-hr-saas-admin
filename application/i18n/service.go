// Package i18n —— 数据文案（统一翻译表）管理用例：通用翻译接口
// （docs《基础设施-数据文案多语言(i18n)设计.md》§7）。
// CRUD 主键是四元组 (resType, resId, field, lang)，不是行 id（§7.1 拒绝记录视角）；
// 写路径只有一条（§7.9）：本包只做 参数校验 → Resolver.Exists → Repo.Save 三件事。
// 标准DDD：只依赖 domain 端口与 pkg/i18n；不触框架。
package i18n

import (
	"context"
	"strconv"

	"dugu-hr-saas-admin/application/types"
	di18n "dugu-hr-saas-admin/domain/i18n"
	"dugu-hr-saas-admin/pkg/i18n"
	"dugu-hr-saas-admin/pkg/utils/log"
)

// audit 审计打点（与 menus 同款）。
func audit(action string, opUID int64, p types.RequestParams, extra map[string]interface{}) {
	props := map[string]interface{}{"operator_id": opUID, "operator_ip": p.IP}
	for k, v := range extra {
		props[k] = v
	}
	log.Audit(action, props)
}

// ---------- 输入/输出对象（接口层直接绑定；本族字段名 camelCase，docs §7.5 JSON 契约） ----------

// MetaView GET /i18n/meta?resType= —— 前端渲染译文编辑 tab 所需的全部静态信息。
type MetaView struct {
	ResType     string           `json:"resType"`
	Label       string           `json:"label"`
	Fields      map[string]int   `json:"fields"`      // 字段 → 长度上限
	Langs       []string         `json:"langs"`       // i18n.Languages() 全量
	DefaultLang string           `json:"defaultLang"` // 不进译文表（§7.4）
	LangBits    map[string]int64 `json:"langBits"`    // 语言→掩码位枚举（bit.go langBits；前端反解 langMask 用，v1.8）
}

// TextsView GET /i18n/texts —— 某资源全部译文（表单回填）。
// texts 里没有 defaultLang 的键；langs 服务端补齐全量（缺语言值 = 空串，非缺键）。
type TextsView struct {
	ResType      string                       `json:"resType"`
	ResID        string                       `json:"resId"` // 雪花ID字符串（语言中立响应约定）
	Label        string                       `json:"label"`
	DefaultLang  string                       `json:"defaultLang"`
	DefaultValue map[string]string            `json:"defaultValue"` // field → 默认语言值（只读展示）
	Langs        []string                     `json:"langs"`
	Texts        map[string]map[string]string `json:"texts"` // field → lang → 译文
}

// PutTextsReq PUT /i18n/texts —— 批量保存（只覆盖所传语言；空串 = 删除该语言）。
type PutTextsReq struct {
	ResType string                       `json:"resType"`
	ResID   string                       `json:"resId"`
	Texts   map[string]map[string]string `json:"texts"` // field → lang → content
}

// BatchDeleteReq POST /i18n/texts/batch-delete —— 批量删除（单事务全成功或全失败）。
type BatchDeleteReq struct {
	Items []DelKeyReq `json:"items"`
}

// DelKeyReq 批量删除的单个四元组。
type DelKeyReq struct {
	ResType string `json:"resType"`
	ResID   string `json:"resId"`
	Field   string `json:"field"`
	Lang    string `json:"lang"`
}

// PendingView GET /i18n/pending —— 缺译清单（游标翻页，无 total，§7.6）。
type PendingView struct {
	ResType string        `json:"resType"`
	Lang    string        `json:"lang"`
	Items   []PendingItem `json:"items"`
}

// PendingItem 缺译条目：source = 默认语言原文（待翻内容），content 恒空串。
type PendingItem struct {
	ResID   string `json:"resId"`
	Key     string `json:"key"` // 资源可读标识（菜单 name / 字典项 value）
	Field   string `json:"field"`
	Source  string `json:"source"`
	Content string `json:"content"`
}

// ---------- 用例 ----------

// Service 统一译文管理。
type Service struct{ t di18n.Repo }

// NewService 构造。
func NewService(t di18n.Repo) *Service { return &Service{t: t} }

// specOf 解析并校验资源注册表（未注册/未挂 Resolver → 30038）。
func specOf(resType string) (di18n.Resource, di18n.Spec, error) {
	r := di18n.Resource(resType)
	s, ok := di18n.SpecOf(r)
	if !ok || s.Resolver == nil {
		return "", di18n.Spec{}, di18n.ErrTargetUnregistered // 30038（含未注册资源——不 panic）
	}
	return r, s, nil
}

// parseResID resId 必须是正整数（雪花字符串）。
func parseResID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, di18n.ErrBadReq // → InvalidParams(400)
	}
	return id, nil
}

// Meta 元信息（无资源校验——meta 本身就是"该资源是否存在"的来源）。
func (s *Service) Meta(ctx context.Context, resType string) (*MetaView, error) {
	r, spec, err := specOf(resType)
	if err != nil {
		return nil, err
	}
	langs := make([]string, 0, 4)
	for _, l := range i18n.Languages() {
		langs = append(langs, string(l))
	}
	bits := make(map[string]int64, 3)
	for l, b := range di18n.Bits() {
		bits[string(l)] = b
	}
	return &MetaView{ResType: string(r), Label: spec.Label, Fields: spec.Fields,
		Langs: langs, DefaultLang: string(i18n.Default()), LangBits: bits}, nil
}

// TextsList GET /i18n/texts（不传 resId）—— 译文总览分页（**DB 原样投影 v1.9**：
// 用户裁定"直接读数据库，不要有任何加工"）——items = sys_i18n_text 行原样：
// texts 是库里存储的 JSON 串（不解码、不补缺译键、不剔默认语言），lang_mask/
// complete/deleted_at/时间原样；不透出 label/langs/defaultLang 等衍生信息
// （meta 接口已承载）。仅保留分页（page/page_size/total）与 deleted_at=0
// 范围过滤——那是查询范围，不是数据加工。id/res_id 字符串化是序列化层契约
// （雪花 19 位超 JS Number 安全整数），不是数据加工。
type TextsList struct {
	Items    []TextsItem `json:"items"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

// TextsItem 译文总览单行：DB 行原样。
type TextsItem struct {
	ID        int64  `json:"id"` // 行雪花ID
	ResType   string `json:"res_type"`
	ResID     string `json:"res_id"`
	Field     string `json:"field"`
	Texts     string `json:"texts"`     // DB 原样 JSON 串
	LangMask  int64  `json:"lang_mask"` // 原样数值（位含义见 meta.langBits）
	Complete  int8   `json:"complete"`  // 原样数值 0/1
	DeletedAt int64  `json:"deleted_at"`
	CreatedAt string `json:"created_at"` // DB 存储形态 yyyy-MM-dd HH:mm:ss.SSS
	UpdatedAt string `json:"updated_at"`
}

// Texts 查（resId 二选一，§7.5 v1.4 起 resId 非必填）：
//   - resId 有值 → 单资源全部译文（详情/表单回填，原语义）；
//   - resId 为空 → 资源类型维度**行列表**（直接读表非聚合 v1.6；正常分页 v1.7：
//     page/page_size，page≤0→1、size≤0→10、>100→100，COUNT+Limit/Offset）。
func (s *Service) Texts(ctx context.Context, resType, field, resIDStr string, page, pageSize int) (interface{}, error) {
	r, spec, err := specOf(resType)
	if err != nil {
		return nil, err
	}
	if resIDStr == "" {
		return s.listTexts(ctx, r, spec, field, page, pageSize)
	}
	return s.textsOf(ctx, r, spec, resIDStr)
}

// listTexts DB 原样投影分页（无解码/无补键/无语言过滤——COUNT+分页两条 SQL）。
func (s *Service) listTexts(ctx context.Context, r di18n.Resource, spec di18n.Spec, field string, page, pageSize int) (*TextsList, error) {
	if field != "" && !di18n.HasField(r, field) {
		return nil, di18n.ErrTargetUnregistered // 30038：field 必须在白名单内
	}
	rows, total, err := s.t.ListRows(ctx, r, field, page, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]TextsItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, TextsItem{
			ID: row.ID, ResType: row.ResType, ResID: strconv.FormatInt(row.ResID, 10),
			Field: row.Field, Texts: row.Texts, LangMask: row.LangMask,
			Complete: row.Complete, DeletedAt: row.DeletedAt,
			CreatedAt: row.CreatedAt.Format("2006-01-02 15:04:05.000"),
			UpdatedAt: row.UpdatedAt.Format("2006-01-02 15:04:05.000"),
		})
	}
	page, size := normalizePageLocal(page, pageSize)
	return &TextsList{Items: items, Total: total, Page: page, PageSize: size}, nil
}

// normalizePageLocal 与 dao.normalizePage 同一规则（page≤0→1、size≤0→10、>100→100）；
// application 层需要 normalize 后的值回显 page/page_size。
func normalizePageLocal(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// textsOf 单资源详情（原语义）。SQL ≤3 条：Exists + Snapshot（每字段 1 条）+ GetMany。
func (s *Service) textsOf(ctx context.Context, r di18n.Resource, spec di18n.Spec, resIDStr string) (*TextsView, error) {
	id, err := parseResID(resIDStr)
	if err != nil {
		return nil, err
	}
	if ok, err := spec.Resolver.Exists(ctx, id); err != nil {
		return nil, err
	} else if !ok {
		return nil, di18n.ErrResNotFound // 30040：目标资源不存在（写前校验同款）
	}
	fields := di18n.Fields(r)
	many, err := s.t.GetMany(ctx, r, []int64{id}, fields, nil)
	if err != nil {
		return nil, err
	}
	def := i18n.Default()
	defaultValue := make(map[string]string, len(fields))
	texts := make(map[string]map[string]string, len(fields))
	langs := i18n.Languages()
	for _, field := range fields { // 每字段：Snapshot 1 条（默认值=主表列，NOT NULL 必有）+ 全 langs 展开缺译=空串
		items, err := spec.Resolver.Snapshot(ctx, []int64{id}, field)
		if err != nil {
			return nil, err
		}
		defaultValue[field] = items[id].Value
		byLang := make(map[string]string, len(langs))
		for _, l := range langs { // langs 全量、缺译 = 空串（验收 12）；默认语言排除（§7.4）
			if l == def {
				continue
			}
			byLang[string(l)] = many[id][field][l]
		}
		texts[field] = byLang
	}
	outLangs := make([]string, 0, len(langs))
	for _, l := range langs {
		outLangs = append(outLangs, string(l))
	}
	return &TextsView{
		ResType: string(r), ResID: strconv.FormatInt(id, 10), Label: spec.Label,
		DefaultLang: string(def), DefaultValue: defaultValue, Langs: outLangs, Texts: texts,
	}, nil
}

// PutTexts 增/改（幂等 PUT）：Exists → 白名单/语言/默认语言 → Repo.Save。
// 存在性校验发生在写入前——库里不新增任何行（验收 9）。
// 返回保存后的全量详情视图（含未动语言；形状随 s.Texts，可能是 *TextsView）。
func (s *Service) PutTexts(ctx context.Context, req *PutTextsReq, opUID int64, p types.RequestParams) (interface{}, error) {
	r, spec, err := specOf(req.ResType)
	if err != nil {
		return nil, err
	}
	id, err := parseResID(req.ResID)
	if err != nil {
		return nil, err
	}
	if len(req.Texts) == 0 {
		return nil, di18n.ErrBadReq
	}
	if ok, err := spec.Resolver.Exists(ctx, id); err != nil {
		return nil, err
	} else if !ok {
		return nil, di18n.ErrResNotFound // 30040
	}
	def := i18n.Default()
	texts := make(map[string]map[di18n.Lang]string, len(req.Texts))
	touched := make([]string, 0, 8) // 审计：field.lang 列表
	for field, byLang := range req.Texts {
		if !di18n.HasField(r, field) {
			return nil, di18n.ErrTargetUnregistered // 30038（白名单外字段直接拒）
		}
		lm := make(map[di18n.Lang]string, len(byLang))
		for lang, content := range byLang {
			l, ok := i18n.ParseLang(lang)
			if !ok {
				return nil, di18n.ErrLangUnsupported // 30035
			}
			if l == def {
				return nil, di18n.ErrDefaultLang // → 400，msg="默认语言请通过资源编辑修改"（§7.4）
			}
			if err := di18n.ValidateTarget(r, field, string(l), content); err != nil {
				return nil, err // 30039（空串合法 = 删除语义）
			}
			lm[l] = content
			touched = append(touched, field+"."+string(l))
		}
		texts[field] = lm
	}
	if err := s.t.Save(ctx, r, id, texts); err != nil {
		return nil, err
	}
	audit("i18n.text.save", opUID, p, map[string]interface{}{
		"res_type": req.ResType, "res_id": req.ResID, "keys": touched})
	return s.Texts(ctx, req.ResType, "", req.ResID, 0, 0) // 保存后回传全量视图（详情模式）
}

// DeleteText 删：单条（三档第 1 档，§7.7）——幂等，删不存在 = 成功无行推进。
func (s *Service) DeleteText(ctx context.Context, resType, resIDStr, field, lang string, opUID int64, p types.RequestParams) error {
	r, _, err := specOf(resType)
	if err != nil {
		return err
	}
	id, err := parseResID(resIDStr)
	if err != nil {
		return err
	}
	if !di18n.HasField(r, field) {
		return di18n.ErrTargetUnregistered
	}
	l, ok := i18n.ParseLang(lang) // 单条删除必须带 lang（三档不在通用 DELETE 上支持"整字段"）
	if lang == "" || !ok {
		return di18n.ErrBadReq
	}
	if l == i18n.Default() {
		return di18n.ErrDefaultLang // 默认语言没有译文行（读走主表），无从删起 → 提示走资源编辑
	}
	if _, err := s.t.BatchDelete(ctx, []di18n.DelKey{{Resource: r, ID: id, Field: field, Lang: l}}); err != nil {
		return err
	}
	audit("i18n.text.delete", opUID, p, map[string]interface{}{
		"res_type": resType, "res_id": resIDStr, "field": field, "lang": lang})
	return nil
}

// BatchDeleteKeys 上限（§7.7：批量删除 ≤200）。
const BatchDeleteKeys = 200

// DeleteBatch 批量删除（三档第 2 档）：上限 200、单事务全成功或全失败。
func (s *Service) DeleteBatch(ctx context.Context, req *BatchDeleteReq, opUID int64, p types.RequestParams) (map[string]interface{}, error) {
	if len(req.Items) == 0 || len(req.Items) > BatchDeleteKeys {
		return nil, di18n.ErrBadReq // 400：超上限是请求形状问题，不占业务码（§13）
	}
	def := i18n.Default()
	keys := make([]di18n.DelKey, 0, len(req.Items))
	for _, item := range req.Items {
		r, spec, err := specOf(item.ResType)
		if err != nil {
			return nil, err
		}
		id, err := parseResID(item.ResID)
		if err != nil {
			return nil, err
		}
		if !di18n.HasField(r, item.Field) {
			return nil, di18n.ErrTargetUnregistered
		}
		l, ok := i18n.ParseLang(item.Lang)
		if !ok {
			return nil, di18n.ErrLangUnsupported
		}
		if l == def {
			return nil, di18n.ErrDefaultLang
		}
		if _, err := spec.Resolver.Exists(ctx, id); err != nil {
			return nil, err
		}
		keys = append(keys, di18n.DelKey{Resource: r, ID: id, Field: item.Field, Lang: l})
	}
	deleted, err := s.t.BatchDelete(ctx, keys)
	if err != nil {
		return nil, err
	}
	audit("i18n.text.batch_delete", opUID, p, map[string]interface{}{
		"requested": len(req.Items), "deleted": deleted})
	return map[string]interface{}{"deleted": deleted}, nil
}

// Pending 缺译清单（§7.6，运营/翻译视角）：三次查询 + 两集合相减。
// 游标分页（afterId）无 total；不做关键词搜索（status 只留 missing）。
func (s *Service) Pending(ctx context.Context, resType, lang string, afterID int64, limit int) (*PendingView, error) {
	r, spec, err := specOf(resType)
	if err != nil {
		return nil, err
	}
	l, ok := i18n.ParseLang(lang)
	if lang == "" || !ok {
		return nil, di18n.ErrBadReq
	}
	if l == i18n.Default() {
		return nil, di18n.ErrDefaultLang // 默认语言恒有主表值，无"缺译"概念
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	ids, err := spec.Resolver.ListIDs(ctx, afterID, limit)
	if err != nil {
		return nil, err
	}
	fields := di18n.Fields(r)
	have, err := s.t.GetMany(ctx, r, ids, fields, []di18n.Lang{l})
	if err != nil {
		return nil, err
	}
	items := make([]PendingItem, 0, len(ids))
	for _, field := range fields {
		snap, err := spec.Resolver.Snapshot(ctx, ids, field)
		if err != nil {
			return nil, err
		}
		for _, id := range ids { // 保持游标顺序（单调递增）
			if _, ok := have[id][field][l]; ok {
				continue // 已有译文
			}
			item := snap[id] // 未命中 id Snapshot 不返回（理论不存在：ids 刚来自 ListIDs）
			if item.ID == 0 {
				continue
			}
			items = append(items, PendingItem{
				ResID: strconv.FormatInt(id, 10), Key: item.Key, Field: field,
				Source: item.Value, Content: "",
			})
		}
	}
	return &PendingView{ResType: string(r), Lang: string(l), Items: items}, nil
}
