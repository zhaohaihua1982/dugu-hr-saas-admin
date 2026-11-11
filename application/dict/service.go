// Package dict —— 字典管理用例（docs/系统管理-字典(sys_dict)设计.md §3/§4/§5/§7）。
// 标准DDD：依赖只有 domain（端口+聚合）与 pkg/i18n；DTO 在本包。
// label 的多语言：主表 label 承担默认语言；其他语言经统一翻译表（labels 只覆盖所传语言，
// 语义与菜单 names 一致，docs 字典设计 §3 取舍 4）。
package dict

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"dugu-hr-saas-admin/application/types"
	authn "dugu-hr-saas-admin/domain/authn"
	ddict "dugu-hr-saas-admin/domain/dict"
	di18n "dugu-hr-saas-admin/domain/i18n"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/i18n"
	"dugu-hr-saas-admin/pkg/utils/log"
)

// ErrBadDictID 字典路径参数不合法（接口层哨兵 → 30046）。
var ErrBadDictID = errors.New("dict: bad id param")

// ---------- 用例输出/输入对象（接口层直接绑定） ----------

// TypeView 类型视图（id 字符串、时间为字符串——语言中立响应约定与 sys_user 同款）。
type TypeView struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Status    int    `json:"status"` // 1启用 0停用
	Remark    string `json:"remark"`
	CreatedAt string `json:"created_at"`
}

// TypeListView 类型分页列表（分页形状与 sys-users/login-logs 同一惯例：
// list/total/page/page_size）。
type TypeListView struct {
	List     []TypeView `json:"list"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// toTypeView 聚合 → 视图。
func toTypeView(t *ddict.Type) TypeView {
	v := TypeView{
		ID: strconv.FormatInt(t.ID, 10), Code: t.Code, Name: t.Name,
		Status: int(t.Status), Remark: t.Remark,
	}
	if !t.CreatedAt.IsZero() {
		v.CreatedAt = t.CreatedAt.Format("2006-01-02 15:04:05")
	}
	return v
}

// OptionView 下拉选项（docs §3 响应形状：前端直接吃 {value,label}，不含 id/remark）。
type OptionView struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ItemView 字典项详情（docs §3：labels 全集供编辑表单回填；不含默认语言——
// 默认语言在主表 label 列，§7.4 两份真相不并存）。
type ItemView struct {
	ID        string            `json:"id"`
	Value     string            `json:"value"`
	Label     string            `json:"label"`
	Labels    map[string]string `json:"labels,omitempty"`
	Sort      int               `json:"sort"`
	Status    int               `json:"status"`
	Remark    string            `json:"remark"`
	CreatedAt string            `json:"created_at"`
}

// toItemView 聚合 → 视图。
func toItemView(i *ddict.Item) ItemView {
	v := ItemView{
		ID:    strconv.FormatInt(i.ID, 10),
		Value: i.Value, Label: i.Label,
		Labels: i.Labels, Sort: i.Sort, Status: int(i.Status), Remark: i.Remark,
	}
	if !i.CreatedAt.IsZero() {
		v.CreatedAt = i.CreatedAt.Format("2006-01-02 15:04:05")
	}
	return v
}

// UpsertTypeReq 创建类型（docs §3）。
type UpsertTypeReq struct {
	Code   string `json:"code" binding:"required,max=64"`
	Name   string `json:"name" binding:"required,max=64"`
	Status *int   `json:"status" binding:"omitempty,oneof=0 1"`
	Remark string `json:"remark" binding:"omitempty,max=255"`
}

// UpdateTypeReq 修改类型（指针语义：传了才改；code 可改但视同接口契约，docs §1.3 警告）。
type UpdateTypeReq struct {
	Code   *string `json:"code" binding:"omitempty,max=64"`
	Name   *string `json:"name" binding:"omitempty,min=1,max=64"`
	Status *int    `json:"status" binding:"omitempty,oneof=0 1"`
	Remark *string `json:"remark" binding:"omitempty,max=255"`
}

// UpsertItemReq 创建字典项（typeCode 而非 type_id，docs §3 取舍 2）。
type UpsertItemReq struct {
	TypeCode string            `json:"typeCode" binding:"required,max=64"`
	Value    string            `json:"value" binding:"required,max=64"`
	Label    string            `json:"label" binding:"required,max=64"`
	Labels   map[string]string `json:"labels"` // lang → 译文；ParseLang 归一化，未注册 → 30035
	Sort     *int              `json:"sort" binding:"omitempty,min=0"`
	Status   *int              `json:"status" binding:"omitempty,oneof=0 1"`
	Remark   string            `json:"remark" binding:"omitempty,max=255"`
}

// UpdateItemReq 修改字典项（指针语义）。**无 typeCode**：项归属类型不可变
// （移动类型不在契约内；uk_dict_item 是 (type_id,value)，悄悄换类型会让历史引用悬空）。
type UpdateItemReq struct {
	Value  *string           `json:"value" binding:"omitempty,max=64"`
	Label  *string           `json:"label" binding:"omitempty,min=1,max=64"`
	Labels map[string]string `json:"labels"` // 只覆盖所传语言
	Sort   *int              `json:"sort" binding:"omitempty,min=0"`
	Status *int              `json:"status" binding:"omitempty,oneof=0 1"`
	Remark *string           `json:"remark" binding:"omitempty,max=255"`
}

// ---------- 用例 ----------

// Service 字典管理用例。
type Service struct {
	types dictTypeRepo
	items dictItemRepo
	ids   authn.IDGenerator
}

// dictTypeRepo / dictItemRepo 领域端口别名（本包引用名）。
type (
	dictTypeRepo = ddict.TypeRepository
	dictItemRepo = ddict.ItemRepository
)

// codeOf 本包内错误 → 业务码（pkg/code 30041~30046 段）。
func codeOf(err error) int {
	switch {
	case err == nil:
		return code.SUCCESS
	case errors.Is(err, ErrBadDictID):
		return code.ErrorDictBadParams
	case errors.Is(err, ddict.ErrTypeNotFound):
		return code.ErrorDictTypeNotFound
	case errors.Is(err, ddict.ErrItemNotFound):
		return code.ErrorDictItemNotFound
	case errors.Is(err, ddict.ErrTypeCodeTaken):
		return code.ErrorDictTypeCodeTaken
	case errors.Is(err, ddict.ErrValueTaken):
		return code.ErrorDictValueTaken
	case errors.Is(err, ddict.ErrTypeHasItems):
		return code.ErrorDictTypeHasItems
	case errors.Is(err, ddict.ErrLangUnsupported), errors.Is(err, di18n.ErrLangUnsupported):
		return code.ErrorMenuLangUnsupported
	case errors.Is(err, ddict.ErrBadParams), errors.Is(err, di18n.ErrContentInvalid):
		return code.ErrorDictBadParams
	case errors.Is(err, ddict.ErrTypeDisabled):
		return code.ErrorDictTypeDisabled
	}
	return code.ErrorDatabase
}

// BadIDCode 路径参数 id 不合法的业务码（api 层直接取用；→ 30046）。
func BadIDCode() int { return code.ErrorDictBadParams }

// NewService 构造。
func NewService(types dictTypeRepo, items dictItemRepo, ids authn.IDGenerator) *Service {
	return &Service{types: types, items: items, ids: ids}
}

// audit 审计打点（与 menus/sysuser 同款；docs 字典设计 §8）。
func (s *Service) audit(action string, opUID int64, p types.RequestParams, detail map[string]interface{}) {
	props := map[string]interface{}{"operator_id": opUID, "operator_ip": p.IP}
	for k, v := range detail {
		props[k] = v
	}
	log.Audit(action, props)
}

// ---------- 类型用例 ----------

// ListTypes 类型分页列表（code 模糊 / status 过滤）。
func (s *Service) ListTypes(ctx context.Context, code string, status *int, page, pageSize int) (*TypeListView, error) {
	q := ddict.ListTypesQuery{Code: strings.TrimSpace(code), Page: page, Size: pageSize}
	if status != nil {
		st := ddict.Status(*status)
		q.Status = &st
	}
	items, total, err := s.types.List(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]TypeView, 0, len(items))
	for i := range items {
		out = append(out, toTypeView(&items[i]))
	}
	return &TypeListView{
		List: out, Total: total,
		Page: normalizePageNo(page), PageSize: normalizePageSize(pageSize),
	}, nil
}

// CreateType 新建类型（code 重复 → 30043）。
func (s *Service) CreateType(ctx context.Context, req *UpsertTypeReq, opUID int64, p types.RequestParams) (*TypeView, error) {
	t := &ddict.Type{
		Code: strings.TrimSpace(req.Code), Name: req.Name, Remark: req.Remark,
		Status: ddict.StatusOn,
	}
	if req.Status != nil {
		t.Status = ddict.Status(*req.Status)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	t.ID = s.ids.New()
	if err := s.types.Create(ctx, t); err != nil {
		return nil, err
	}
	s.audit("dict.type.create", opUID, p, map[string]interface{}{
		"dict_type_id": t.ID, "code": t.Code, "name": t.Name,
	})
	v := toTypeView(t)
	return &v, nil
}

// UpdateType 修改类型（不存在 → 30041；code 重复 → 30043）。
func (s *Service) UpdateType(ctx context.Context, id int64, req *UpdateTypeReq, opUID int64, p types.RequestParams) (*TypeView, error) {
	t, err := s.loadType(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Code != nil {
		t.Code = strings.TrimSpace(*req.Code)
	}
	if req.Name != nil {
		t.Name = *req.Name
	}
	if req.Status != nil {
		t.Status = ddict.Status(*req.Status)
	}
	if req.Remark != nil {
		t.Remark = *req.Remark
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := s.types.Save(ctx, t); err != nil {
		return nil, err
	}
	s.audit("dict.type.update", opUID, p, map[string]interface{}{
		"dict_type_id": id, "code": t.Code,
	})
	v := toTypeView(t)
	return &v, nil
}

// DeleteType 删除类型（有项 → 30045，不做级联软删，docs §5）。
func (s *Service) DeleteType(ctx context.Context, id int64, opUID int64, p types.RequestParams) error {
	t, err := s.loadType(ctx, id)
	if err != nil {
		return err
	}
	cnt, err := s.types.CountItems(ctx, id)
	if err != nil {
		return err
	}
	if cnt > 0 {
		return ddict.ErrTypeHasItems
	}
	if err := s.types.Delete(ctx, id); err != nil {
		return err
	}
	s.audit("dict.type.delete", opUID, p, map[string]interface{}{
		"dict_type_id": id, "code": t.Code, "item_count": cnt,
	})
	return nil
}

// ---------- 项用例 ----------

// ListItems 类型下项列表（前端下拉直接吃，docs §3）：
// type=code 定位类型（拼错 → 30046，调用方 bug 必须暴露）；只返回启用项，
// withDisabled=1 才带停用项；?lang= 决定展示 label（缺省默认语言，缺译回退主表列）。
func (s *Service) ListItems(ctx context.Context, typeCode, langQuery string, withDisabled bool) ([]OptionView, error) {
	items, err := s.listItems(ctx, typeCode, langQuery, withDisabled, false)
	if err != nil {
		return nil, err
	}
	out := make([]OptionView, 0, len(items))
	for i := range items {
		out = append(out, OptionView{Value: items[i].Value, Label: items[i].DisplayLabel(resolveLang(langQuery))})
	}
	return out, nil
}

// ListItemsManage 管理视图（docs §3 v1.5 契约：?manage=1 → 完整 ItemView，
// 含 id/sort/status/remark——后台编辑/删除/启停/多语言入口需要真实主键）。
// 只供后台字典管理页；普通业务下拉不得传 manage=1（契约保持最小 {value,label}）。
// manage=1 本身即管理通道：放行"停用类型"检查（管理停用类型的项），停用项仍由
// withDisabled 控制。
func (s *Service) ListItemsManage(ctx context.Context, typeCode, langQuery string, withDisabled bool) ([]ItemView, error) {
	items, err := s.listItems(ctx, typeCode, langQuery, withDisabled, true)
	if err != nil {
		return nil, err
	}
	lang := resolveLang(langQuery)
	out := make([]ItemView, 0, len(items))
	for i := range items {
		v := toItemView(&items[i])
		v.Label = items[i].DisplayLabel(lang) // 管理列表按请求语言显示（缺译回退主表列）
		v.Labels = nil                        // 列表不回 labels（docs §3：只有详情回，前端译文入口走 i18n texts 接口）
		out = append(out, v)
	}
	return out, nil
}

// listItems 公共读取路径：类型定位 → 停用检查（manage/withDisabled 放行）→ 语言解析 → 列表。
func (s *Service) listItems(ctx context.Context, typeCode, langQuery string, withDisabled, manage bool) ([]ddict.Item, error) {
	t, err := s.loadTypeByCode(ctx, typeCode)
	if err != nil {
		return nil, err
	}
	// 类型停用 → 连带项不下发（用户裁定 v1.4，docs §2）；withDisabled=1 / manage=1
	// 是管理回显通道（编辑页取全量），放行类型停用检查与停用项下发。
	if !withDisabled && !manage && t.Status != ddict.StatusOn {
		return nil, ddict.ErrTypeDisabled
	}
	lang := resolveLang(langQuery)
	return s.items.ListByType(ctx, t.ID, withDisabled, lang)
}

// ItemDetail 项详情（labels 全集，编辑页回填；docs §3——列表接口不回 labels，详情才回）。
func (s *Service) ItemDetail(ctx context.Context, id int64) (*ItemView, error) {
	i, err := s.loadItem(ctx, id)
	if err != nil {
		return nil, err
	}
	v := toItemView(i)
	return &v, nil
}

// CreateItem 新建字典项（typeCode 不存在 → 30046；value 同类型重复 → 30044）。
func (s *Service) CreateItem(ctx context.Context, req *UpsertItemReq, opUID int64, p types.RequestParams) (*ItemView, error) {
	t, err := s.loadTypeByCode(ctx, req.TypeCode)
	if err != nil {
		return nil, err
	}
	labels, err := normalizeLabels(req.Labels)
	if err != nil {
		return nil, err
	}
	if err := checkLabelDefaultLang(req.Label, labels); err != nil {
		return nil, err
	}
	i := &ddict.Item{
		TypeID: t.ID, Value: req.Value, Label: req.Label, Labels: labels,
		Status: ddict.StatusOn,
	}
	if req.Sort != nil {
		i.Sort = *req.Sort
	}
	if req.Status != nil {
		i.Status = ddict.Status(*req.Status)
	}
	i.Remark = req.Remark
	if err := i.Validate(); err != nil {
		return nil, err
	}
	i.ID = s.ids.New()
	if err := s.items.Create(ctx, i); err != nil {
		return nil, err
	}
	s.audit("dict.item.create", opUID, p, map[string]interface{}{
		"dict_item_id": i.ID, "type_code": t.Code, "value": i.Value, "label": i.Label,
	})
	loaded, lErr := s.loadItem(ctx, i.ID)
	if lErr != nil {
		return nil, lErr
	}
	v := toItemView(loaded)
	return &v, nil
}

// UpdateItem 修改字典项（不存在 → 30042；value 重复 → 30044；labels 只覆盖所传语言）。
func (s *Service) UpdateItem(ctx context.Context, id int64, req *UpdateItemReq, opUID int64, p types.RequestParams) (*ItemView, error) {
	i, err := s.loadItem(ctx, id) // 已含 labels 全集
	if err != nil {
		return nil, err
	}
	if req.Value != nil {
		i.Value = *req.Value
	}
	if req.Label != nil {
		i.Label = *req.Label
	}
	if req.Sort != nil {
		i.Sort = *req.Sort
	}
	if req.Status != nil {
		i.Status = ddict.Status(*req.Status)
	}
	if req.Remark != nil {
		i.Remark = *req.Remark
	}
	if len(req.Labels) > 0 {
		patch, err := normalizeLabels(req.Labels)
		if err != nil {
			return nil, err
		}
		if err := checkLabelDefaultLang(i.Label, patch); err != nil {
			return nil, err
		}
		i.Labels = mergeLabels(i.Labels, patch)
		if err := s.items.UpsertLabels(ctx, id, patch); err != nil {
			return nil, err
		}
	}
	if err := i.Validate(); err != nil { // 全字段终检
		return nil, err
	}
	if err := s.items.Save(ctx, i); err != nil {
		return nil, err
	}
	s.audit("dict.item.update", opUID, p, map[string]interface{}{
		"dict_item_id": id, "value": i.Value, "label": i.Label,
	})
	updated, uErr := s.loadItem(ctx, id)
	if uErr != nil {
		return nil, uErr
	}
	v := toItemView(updated)
	return &v, nil
}

// DeleteItem 删除字典项（软删主表 + 统一表译文行同事务）。
func (s *Service) DeleteItem(ctx context.Context, id int64, opUID int64, p types.RequestParams) error {
	i, err := s.loadItem(ctx, id)
	if err != nil {
		return err
	}
	if err := s.items.Delete(ctx, id); err != nil {
		return err
	}
	s.audit("dict.item.delete", opUID, p, map[string]interface{}{
		"dict_item_id": id, "value": i.Value, "label": i.Label,
	})
	return nil
}

// StatusReq 独立启停端点请求体（v1.4 用户裁定：POST :id/status，body {status:0|1}）。
type StatusReq struct {
	Status *int `json:"status" binding:"required,oneof=0 1"`
}

// SetTypeStatus 启停类型（独立端点）。停用即时生效：其下字典项连带不下发（30047）。
func (s *Service) SetTypeStatus(ctx context.Context, id int64, req *StatusReq, opUID int64, p types.RequestParams) error {
	t, err := s.loadType(ctx, id)
	if err != nil {
		return err
	}
	t.Status = ddict.Status(*req.Status)
	if err := t.Validate(); err != nil {
		return err
	}
	if err := s.types.Save(ctx, t); err != nil {
		return err
	}
	s.audit("dict.type.status", opUID, p, map[string]interface{}{
		"dict_type_id": id, "code": t.Code, "status": int(t.Status),
	})
	return nil
}

// SetItemStatus 启停字典项（独立端点）。停用即"不再提供新选择"，历史数据仍引用
// （docs §2 停用 ≠ 删除）。
func (s *Service) SetItemStatus(ctx context.Context, id int64, req *StatusReq, opUID int64, p types.RequestParams) error {
	i, err := s.loadItem(ctx, id)
	if err != nil {
		return err
	}
	i.Status = ddict.Status(*req.Status)
	if err := i.Validate(); err != nil {
		return err
	}
	if err := s.items.Save(ctx, i); err != nil {
		return err
	}
	s.audit("dict.item.status", opUID, p, map[string]interface{}{
		"dict_item_id": id, "value": i.Value, "status": int(i.Status),
	})
	return nil
}

// ---------- 内部 ----------

// loadType 加载类型；不存在 → ErrTypeNotFound（30041）。
func (s *Service) loadType(ctx context.Context, id int64) (*ddict.Type, error) {
	t, err := s.types.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ddict.ErrTypeNotFound
	}
	return t, nil
}

// loadTypeByCode 按 code 定位类型；不存在 → ErrBadParams（30046——拼错 code 是调用方 bug，
// docs §2 读取语义：类型不存在必须暴露，不是空集）。
func (s *Service) loadTypeByCode(ctx context.Context, typeCode string) (*ddict.Type, error) {
	t, err := s.types.ByCode(ctx, strings.TrimSpace(typeCode))
	if err != nil {
		return nil, err
	}
	if t == nil {
		// 拼错/不存在的 typeCode（30046 带字段提示，docs 字典设计 §4）
		return nil, &ddict.BadField{Field: "typeCode", Err: ddict.ErrBadParams}
	}
	return t, nil
}

// loadItem 加载项；不存在 → ErrItemNotFound（30042）。
func (s *Service) loadItem(ctx context.Context, id int64) (*ddict.Item, error) {
	i, err := s.items.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if i == nil {
		return nil, ddict.ErrItemNotFound
	}
	return i, nil
}

// resolveLang ?lang= 解析（读宽容：未识别回退默认语言，不报错）。
func resolveLang(langQuery string) i18n.Lang {
	if l, ok := i18n.ParseLang(langQuery); ok {
		return l
	}
	return i18n.Default()
}

// normalizeLabels 归一化 labels key（ParseLang：en_us → en-US），未注册 → 30035。
// 与菜单 normalizeNames 同一规则（docs 字典设计 §11 验收 7：传 en_us 归一化后正常保存）。
func normalizeLabels(labels map[string]string) (map[string]string, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(labels))
	langs := i18n.Languages() // 上限校验：单次提交语言数 ≤ 已注册语言数
	if len(labels) > len(langs) {
		return nil, &ddict.BadField{Field: "labels", Err: ddict.ErrBadParams}
	}
	for k, v := range labels {
		l, ok := i18n.ParseLang(k)
		if !ok {
			return nil, ddict.ErrLangUnsupported
		}
		out[string(l)] = v
	}
	return out, nil
}

// checkLabelDefaultLang 默认语言译文与主表 label 必须一致（docs §7 不变式 3；
// 与菜单 checkNameDefaultLang 同一规则）。labels 不含默认语言 = 未提供，缺省放行；
// 显式提供则必须相等（相等时统一表写入路径自动剔除默认语言键，§7.4）。
func checkLabelDefaultLang(label string, labels map[string]string) error {
	def := string(i18n.Default())
	if v, ok := labels[def]; ok && strings.TrimSpace(v) != strings.TrimSpace(label) {
		return &ddict.BadField{Field: "labels." + def, Err: ddict.ErrBadParams}
	}
	if strings.TrimSpace(label) == "" {
		return &ddict.BadField{Field: "label", Err: ddict.ErrBadParams}
	}
	return nil
}

// mergeLabels 聚合现值 + 本次提交（仅覆盖所传语言）。
func mergeLabels(base, patch map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(patch))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range patch {
		out[k] = v
	}
	return out
}

// normalizePageNo / normalizePageSize 分页参数归一（与 dao.normalizePage 同一规则，
// 视图层回显用；dao 层入库前还会再归一一次）。
func normalizePageNo(page int) int {
	if page <= 0 {
		return 1
	}
	return page
}

func normalizePageSize(size int) int {
	if size <= 0 {
		return 10
	}
	if size > 100 {
		return 100
	}
	return size
}
