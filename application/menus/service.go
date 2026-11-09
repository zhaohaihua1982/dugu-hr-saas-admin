// Package menu —— 菜单管理用例（docs 三模块设计 §1/§1.1/§4 + 管理图 menu.jpg 转化）。
// 标准DDD：依赖只有 domain（端口+聚合）与 pkg/i18n；DTO 在本包。
package menu

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"dugu-hr-saas-admin/application/types"
	authn "dugu-hr-saas-admin/domain/authn"
	dmenu "dugu-hr-saas-admin/domain/menu"
	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/i18n"
	"dugu-hr-saas-admin/pkg/utils/log"
)

// ErrBadMenuID 菜单路径参数不合法（接口层哨兵 → 30034）。
var ErrBadMenuID = errors.New("menus: bad id param")

// ---------- 用例输出/输入对象（接口层直接绑定） ----------

// NodeView 菜单节点视图（语言中立响应约定与 sys_user 同款：id 字符串、时间为字符串）。
// Name 为 ?lang= 解析后的展示名（回退链：译文 → 主表 name）；
// Names 仅在详情/编辑回填时填充；Children 仅树形状填充（'[]' 非 null）。
type NodeView struct {
	ID         string            `json:"id"`
	ParentID   string            `json:"parent_id"`
	Name       string            `json:"name"`
	Names      map[string]string `json:"names,omitempty"`
	Type       int               `json:"type"` // 1目录 2菜单 3动作
	Path       string            `json:"path"`
	Component  string            `json:"component"`
	Icon       string            `json:"icon"`
	Sort       int               `json:"sort"`
	Visible    bool              `json:"visible"`
	Status     int               `json:"status"` // 1启用 0停用
	ActionCode string            `json:"action_code"`
	Remark     string            `json:"remark"`
	CreatedAt  string            `json:"created_at"`
	Children   []NodeView        `json:"children,omitempty"`
}

// toView 聚合 → 视图（lang 为空表示不解析展示名 → 直接主表 name）。
func toView(m *dmenu.Menu, lang string) NodeView {
	v := NodeView{
		ID: strconv.FormatInt(m.ID, 10), ParentID: strconv.FormatInt(m.ParentID, 10),
		Name: m.Name, Type: int(m.Type),
		Path: m.Path, Component: m.Component, Icon: m.Icon,
		Sort: m.Sort, Visible: m.Visible, Status: int(m.Status),
		ActionCode: m.ActionCode, Remark: m.Remark,
	}
	if lang != "" {
		v.Name = m.DisplayName(i18n.Lang(lang))
	}
	if m.Names != nil {
		v.Names = m.Names
	}
	if !m.CreatedAt.IsZero() {
		v.CreatedAt = m.CreatedAt.Format("2006-01-02 15:04:05")
	}
	return v
}

// UpsertMenuReq 创建/修改公共字段（修改时 UpdateMenuReq 用指针语义：传了才改）。
type UpsertMenuReq struct {
	ParentID   string            `json:"parent_id" binding:"omitempty"` // "0"或空=根
	Name       string            `json:"name" binding:"required,max=64"`
	Names      map[string]string `json:"names"` // lang → 名；写入前 ParseLang 归一化，未注册 → 30035
	Type       int               `json:"type" binding:"required,oneof=1 2 3"`
	Path       string            `json:"path" binding:"omitempty,max=128"`
	Component  string            `json:"component" binding:"omitempty,max=128"`
	Icon       string            `json:"icon" binding:"omitempty,max=64"`
	Sort       int               `json:"sort" binding:"omitempty,min=0,max=9999"`
	Visible    *bool             `json:"visible"` // 缺省 true
	Status     *int              `json:"status" binding:"omitempty,oneof=0 1"`
	ActionCode string            `json:"action_code" binding:"omitempty,max=64"`
	Remark     string            `json:"remark" binding:"omitempty,max=255"`
}

// UpdateMenuReq 修改（指针语义）。
type UpdateMenuReq struct {
	ParentID   *string           `json:"parent_id" binding:"omitempty"`
	Name       *string           `json:"name" binding:"omitempty,min=1,max=64"`
	Names      map[string]string `json:"names"` // 只覆盖所传语言（docs §4）
	Type       *int              `json:"type" binding:"omitempty,oneof=1 2 3"`
	Path       *string           `json:"path" binding:"omitempty,max=128"`
	Component  *string           `json:"component" binding:"omitempty,max=128"`
	Icon       *string           `json:"icon" binding:"omitempty,max=64"`
	Sort       *int              `json:"sort" binding:"omitempty,min=0,max=9999"`
	Visible    *bool             `json:"visible"`
	Status     *int              `json:"status" binding:"omitempty,oneof=0 1"`
	ActionCode *string           `json:"action_code" binding:"omitempty,max=64"`
	Remark     *string           `json:"remark" binding:"omitempty,max=255"`
}

// SortItem 保存排序条目（管理图“保存排序”：前端整树按行提交 {id, sort}）。
type SortItem struct {
	ID   string `json:"id"`
	Sort int    `json:"sort"`
}

// SortReq 批量排序请求（≤500 条，覆盖整树一次保存）。
type SortReq struct {
	Items []SortItem `json:"items" binding:"required,min=1,max=500"`
}

// SortItemResult 批量排序逐项结果（独立处理，部分失败不影响已成功项——与冻结批量同风格）。
type SortItemResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Code    int    `json:"code,omitempty"`
}

// ---------- 用例 ----------

// Service 菜单管理用例。
type Service struct {
	menus dmenu.Repository
	ids   authn.IDGenerator
}

// codeOf 本包内错误 → 业务码（pkg/code 三处同步的 30031~30036 段）。
// 注：application 根包的 CodeOf 会统一做同一映射（api 层用）；批量排序的
// 逐项结果在本包取码，避免 application→子包 的反向 import 环。
func codeOf(err error) int {
	switch {
	case err == nil:
		return code.SUCCESS
	case errors.Is(err, ErrBadMenuID), errors.Is(err, dmenu.ErrNotFound):
		return code.ErrorMenuNotFound
	case errors.Is(err, dmenu.ErrHasChildren):
		return code.ErrorMenuHasChildren
	case errors.Is(err, dmenu.ErrActionCodeTaken):
		return code.ErrorMenuActionCodeTaken
	case errors.Is(err, dmenu.ErrLangUnsupported):
		return code.ErrorMenuLangUnsupported
	case errors.Is(err, dmenu.ErrNameRequired):
		return code.ErrorMenuNameRequired
	case errors.Is(err, dmenu.ErrBadParams):
		return code.ErrorMenuBadParams
	}
	return code.ErrorDatabase
}

// NewService 构造。
func NewService(menus dmenu.Repository, ids authn.IDGenerator) *Service {
	return &Service{menus: menus, ids: ids}
}

// audit 审计打点（与 sysuser 同款，docs：不做配置变更审计表）。
func (s *Service) audit(action string, opUID int64, p types.RequestParams, detail map[string]interface{}) {
	props := map[string]interface{}{"operator_id": opUID, "operator_ip": p.IP}
	for k, v := range detail {
		props[k] = v
	}
	log.Audit(action, props)
}

// Tree 全量树（docs §4：不过滤、不做权限控制；?lang= 决定展示名）。
// 无 lang 参数 → 用默认语言；未识别 → 也回退默认语言（不报错，读宽容写严格）。
func (s *Service) Tree(ctx context.Context, langQuery string) ([]NodeView, error) {
	lang := i18n.Default()
	if l, ok := i18n.ParseLang(langQuery); ok {
		lang = l
	}
	all, err := s.menus.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	names, err := s.menus.NamesByIDs(ctx, idsOf(all))
	if err != nil {
		return nil, err
	}
	attach := func(m *dmenu.Menu) *NodeView {
		m.Names = names[m.ID]
		v := toView(m, string(lang))
		v.Names = nil // docs §1.1：树/展示只回一个 name，names 仅详情/编辑回填
		return &v
	}
	// ListAll 已按 (sort, id) 排序 → 同父子孙相对有序
	idx := make(map[int64][]int, len(all))
	for i := range all {
		idx[all[i].ParentID] = append(idx[all[i].ParentID], i)
	}
	var build func(parentID int64) []NodeView
	build = func(parentID int64) []NodeView {
		kids := idx[parentID]
		out := make([]NodeView, 0, len(kids))
		for _, k := range kids {
			v := attach(&all[k])
			v.Children = build(all[k].ID)
			out = append(out, *v)
		}
		return out
	}
	return build(0), nil
}

// Detail 详情（含 names 全集，编辑页回填用；docs §4）。
func (s *Service) Detail(ctx context.Context, id int64) (*NodeView, error) {
	m, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	v := toView(m, "") // 主表 name（默认语言）；names 全集已在聚合上
	return &v, nil
}

// Create 创建节点（含动作管理与多语言 names）。
func (s *Service) Create(ctx context.Context, req *UpsertMenuReq, opUID int64, p types.RequestParams) (*NodeView, error) {
	parentID, err := parseParentID(req.ParentID)
	if err != nil {
		return nil, err
	}
	names, err := normalizeNames(req.Names)
	if err != nil {
		return nil, err
	}
	if err := checkNameDefaultLang(req.Name, names); err != nil {
		return nil, err
	}
	m := &dmenu.Menu{
		ParentID: parentID, Name: req.Name, Names: names,
		Type: dmenu.Type(req.Type),
		Path: req.Path, Component: req.Component, Icon: req.Icon,
		Sort: req.Sort, Visible: req.Visible == nil || *req.Visible,
		ActionCode: strings.TrimSpace(req.ActionCode), Remark: req.Remark,
	}
	if req.Status != nil {
		m.Status = dmenu.Status(*req.Status)
	} else {
		m.Status = dmenu.StatusOn
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if err := s.checkParent(ctx, m.ParentID, 0); err != nil {
		return nil, err
	}
	if err := s.checkDepth(ctx, m.ParentID, 0); err != nil {
		return nil, err
	}
	m.ID = s.ids.New()
	if err := s.menus.Create(ctx, m); err != nil {
		return nil, err
	}
	s.audit("menu.create", opUID, p, map[string]interface{}{
		"menu_id": m.ID, "name": m.Name, "type": int(m.Type),
		"parent_id": m.ParentID, "action_code": m.ActionCode, "langs": keysOf(names),
	})
	loaded, lErr := s.load(ctx, m.ID)
	if lErr != nil {
		return nil, lErr
	}
	v := toView(loaded, "")
	return &v, nil
}

// Update 修改（指针语义：传了才改；names 只覆盖所传语言）。
func (s *Service) Update(ctx context.Context, id int64, req *UpdateMenuReq, opUID int64, p types.RequestParams) (*NodeView, error) {
	m, err := s.load(ctx, id) // 已含 names 全集
	if err != nil {
		return nil, err
	}

	// 换类型：有子节点禁止（动作是叶子等不变式会被破坏，docs §5 30032 同源）
	if req.Type != nil && int(*req.Type) != int(m.Type) {
		has, err := s.menus.HasChildren(ctx, id)
		if err != nil {
			return nil, err
		}
		if has {
			return nil, dmenu.ErrHasChildren
		}
		m.Type = dmenu.Type(*req.Type)
	}
	// 移动：parent_id 传了且不同 → 结构校验
	if req.ParentID != nil {
		newParentID, pErr := parseParentID(*req.ParentID)
		if pErr != nil {
			return nil, pErr
		}
		if newParentID != m.ParentID {
			if err := s.checkParent(ctx, newParentID, id); err != nil {
				return nil, err
			}
			if err := s.checkDepth(ctx, newParentID, id); err != nil {
				return nil, err
			}
			if err := s.menus.Move(ctx, id, newParentID); err != nil {
				return nil, err
			}
			m.ParentID = newParentID
		}
	}
	// 内容字段：指针语义
	if req.Name != nil {
		m.Name = *req.Name
	}
	if req.Path != nil {
		m.Path = *req.Path
	}
	if req.Component != nil {
		m.Component = *req.Component
	}
	if req.Icon != nil {
		m.Icon = *req.Icon
	}
	if req.Sort != nil {
		m.Sort = *req.Sort
	}
	if req.Visible != nil {
		m.Visible = *req.Visible
	}
	if req.Status != nil {
		m.Status = dmenu.Status(*req.Status)
	}
	if req.ActionCode != nil {
		m.ActionCode = strings.TrimSpace(*req.ActionCode)
	}
	if req.Remark != nil {
		m.Remark = *req.Remark
	}
	// names：只覆盖所传语言；默认语言译文若所传，必须与（新）name 一致
	if len(req.Names) > 0 {
		names, err := normalizeNames(req.Names)
		if err != nil {
			return nil, err
		}
		if err := checkNameDefaultLang(m.Name, names); err != nil {
			return nil, err
		}
		m.Names = mergeNames(m.Names, names)
		if err := s.menus.UpsertNames(ctx, id, names); err != nil {
			return nil, err
		}
		// 主表 name 若因默认语言语义需同步（names 含默认语言且 != name），在 checkNameDefaultLang 已拦截
	}
	if err := m.Validate(); err != nil { // 全字段终检（含 type/action_code 对偶）
		return nil, err
	}
	if err := s.menus.Save(ctx, m); err != nil {
		return nil, err
	}
	s.audit("menu.update", opUID, p, map[string]interface{}{
		"menu_id": id, "type": int(m.Type), "parent_id": m.ParentID, "action_code": m.ActionCode,
	})
	updated, uErr := s.load(ctx, id)
	if uErr != nil {
		return nil, uErr
	}
	v := toView(updated, "")
	return &v, nil
}

// Delete 删除（有子节点 → 30032；软删含译文子表）。
func (s *Service) Delete(ctx context.Context, id int64, opUID int64, p types.RequestParams) error {
	m, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	has, err := s.menus.HasChildren(ctx, id)
	if err != nil {
		return err
	}
	if has {
		return dmenu.ErrHasChildren
	}
	if err := s.menus.Delete(ctx, id); err != nil {
		return err
	}
	s.audit("menu.delete", opUID, p, map[string]interface{}{"menu_id": id, "name": m.Name})
	return nil
}

// Sort 批量保存排序（管理图“保存排序”）。逐项独立：不存在的 id 该项 30031。
func (s *Service) Sort(ctx context.Context, req *SortReq, opUID int64, p types.RequestParams) []SortItemResult {
	out := make([]SortItemResult, 0, len(req.Items))
	for _, it := range req.Items {
		item := SortItemResult{ID: it.ID}
		id, err := strconv.ParseInt(it.ID, 10, 64)
		if err != nil {
			item.Code = codeOf(dmenu.ErrBadParams)
			out = append(out, item)
			continue
		}
		m, err := s.menus.ByID(ctx, id)
		if err != nil {
			item.Code = codeOf(err)
			out = append(out, item)
			continue
		}
		if m == nil {
			item.Code = codeOf(dmenu.ErrNotFound)
			out = append(out, item)
			continue
		}
		if err := s.menus.UpdateSort(ctx, id, it.Sort); err != nil {
			item.Code = codeOf(err)
		} else {
			item.Success = true
		}
		out = append(out, item)
	}
	s.audit("menu.sort", opUID, p, map[string]interface{}{"count": len(req.Items)})
	return out
}

// ---------- 内部 ----------

// load 加载节点；不存在 → ErrNotFound（30031）。
func (s *Service) load(ctx context.Context, id int64) (*dmenu.Menu, error) {
	m, err := s.menus.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, dmenu.ErrNotFound
	}
	return m, nil
}

// normalizeNames 归一化 names key（ParseLang：en_us → en-US），未注册 → 30035。
func normalizeNames(names map[string]string) (map[string]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(names))
	langs := i18n.Languages() // 上限校验：单次提交语言数 ≤ 已注册语言数
	if len(names) > len(langs) {
		return nil, dmenu.ErrBadParams
	}
	for k, v := range names {
		l, ok := i18n.ParseLang(k)
		if !ok {
			return nil, dmenu.ErrLangUnsupported
		}
		out[string(l)] = v
	}
	return out, nil
}

// checkNameDefaultLang 默认语言译文与主表 name 必须一致（docs §1.1 约束 4）：
// names 不含默认语言 = 未提供，缺省视为 name 本身；显式提供则必须相等。
func checkNameDefaultLang(name string, names map[string]string) error {
	def := string(i18n.Default())
	if v, ok := names[def]; ok && strings.TrimSpace(v) != strings.TrimSpace(name) {
		// docs §1.1 约束4：主表 name 必须等于默认语言译文；不一致=参数错误（30034）
		return dmenu.ErrBadParams
	}
	if strings.TrimSpace(name) == "" {
		return dmenu.ErrNameRequired
	}
	return nil
}

// mergeNames 聚合现值 + 本次提交（仅覆盖所传语言）。
func mergeNames(base, patch map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(patch))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range patch {
		out[k] = v
	}
	return out
}

// parseParentID "0"/""=0；其余须为雪花字符串。
func parseParentID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, dmenu.ErrBadParams
	}
	return id, nil
}

// checkParent 父结构校验：存在、不是动作（动作是叶子）、不能是自己。
func (s *Service) checkParent(ctx context.Context, parentID, selfID int64) error {
	if parentID == 0 {
		return nil
	}
	if parentID == selfID {
		return dmenu.ErrBadParams
	}
	p, err := s.menus.ByID(ctx, parentID)
	if err != nil {
		return err
	}
	if p == nil {
		return dmenu.ErrBadParams
	}
	if p.Type == dmenu.TypeAction {
		return dmenu.ErrBadParams
	}
	return nil
}

// checkDepth 沿父链上溯：命中 selfID=成环；深度 > MaxDepth=超限。
// 全量一次 ListAll 内存判定（菜单是百级数据，避免逐层查询）。
func (s *Service) checkDepth(ctx context.Context, newParentID, selfID int64) error {
	all, err := s.menus.ListAll(ctx)
	if err != nil {
		return err
	}
	parentOf := make(map[int64]int64, len(all))
	for i := range all {
		parentOf[all[i].ID] = all[i].ParentID
	}
	depth, cur := 0, newParentID
	for cur != 0 {
		if cur == selfID {
			return dmenu.ErrBadParams
		}
		p, ok := parentOf[cur]
		if !ok {
			return dmenu.ErrBadParams
		}
		cur = p
		depth++
		if depth > dmenu.MaxDepth {
			return dmenu.ErrBadParams
		}
	}
	return nil
}

// idsOf 提取节点 ID 集合。
func idsOf(all []dmenu.Menu) []int64 {
	out := make([]int64, 0, len(all))
	for i := range all {
		out = append(out, all[i].ID)
	}
	return out
}

// keysOf names 语言 key 集合（审计用）。
func keysOf(names map[string]string) []string {
	out := make([]string, 0, len(names))
	for k := range names {
		out = append(out, k)
	}
	return out
}
