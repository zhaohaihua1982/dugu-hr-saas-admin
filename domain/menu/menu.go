// Package menu —— sys_menu 聚合（导航菜单树 + 动作节点 + 名称多语言）。
//
// 标准DDD分层约束：零框架依赖（不 import gin/gorm/redis/config）。
// pkg/i18n 是纯标准库语言标签工具，被 domain 引用是设计稿（三模块设计 §1.1/§7）
// 的明确约定：语言集合从 i18n.Registered() 派生。
// 存储（主表 sys_menu + 译文子表 sys_menu_i18n）以 Repository 端口定义在此，
// 由 infrastructure/db/dao 实现（DIP）。
package menu

import (
	"context"
	"errors"
	"strings"
	"time"

	"dugu-hr-saas-admin/pkg/i18n"
)

// ---------- 聚合内错误（领域语言；业务码见 pkg/code 30031~30036） ----------

var (
	// ErrNotFound 菜单不存在（含软删）→ 30031。
	ErrNotFound = errors.New("menu: not found")
	// ErrHasChildren 存在子节点 → 30032（禁止删除；有子节点时也禁止改 type——
	// 会破坏“动作是叶子”等不变式，与删除共用“先处理子节点”语义）。
	ErrHasChildren = errors.New("menu: has children")
	// ErrSameLevelTaken 同级重复（v0.3 定稿）：同名/同请求地址/同权限点 → 30037。
	ErrSameLevelTaken = errors.New("menu: same-level duplicate")
	// ErrBadParams 菜单参数不合法 → 30034：字段超限、type/字段组合错误、
	// 父节点无效（不存在/是动作/自己或后代当父）、树深超限等。
	ErrBadParams = errors.New("menu: bad params")
	// ErrLangUnsupported 语言不被支持（names 的 key 须在 i18n.Registered() 内）→ 30035。
	ErrLangUnsupported = errors.New("menu: lang unsupported")
	// ErrNameRequired 默认语言名称（主表 name）不能为空 → 30036。
	ErrNameRequired = errors.New("menu: name required")
)

// ---------- 类型（不变式，docs 三模块设计 §1） ----------

// Type 1目录 2菜单 3动作(action)。
type Type int8

const (
	TypeDir    Type = 1
	TypeMenu   Type = 2
	TypeAction Type = 3
)

// Status 1启用 0停用。
type Status int8

const (
	StatusOn  Status = 1
	StatusOff Status = 0
)

// MaxDepth 树最大层级 = 3。**只统计目录/菜单**（动作 type=3 不计入层级，
// docs：action 数据不算菜单；动作永远依附在目录/菜单下且自身是叶子）。
// 父链上 dir/menu 节点数 ≥ 3 时，不能再挂目录/菜单（超限 → 30034）。
const MaxDepth = 3

// 字段长度上限（与 DDL 一致）。
const (
	maxName   = 64
	maxPath   = 128
	maxIcon   = 64
	maxCode   = 64 // action_code VALUES(64) 与 DDL 同步
	maxRemark = 255
)

// ---------- 聚合根 ----------

// Menu 菜单聚合根。不变式（v0.3 同级唯一方案定稿）：
//   - 所有节点三必填：name（默认语言名兜底）、**path（请求地址）**、
//     **action_code（权限点）**；
//   - 同级唯一（同一 parent 下）：name / path / action_code 互不重复
//     （DB 生成列 uk_menu_name/uk_menu_path/uk_menu_action，软删行不参与）；
//   - 按钮无独立路由：path 约定填 action_code 同值（同级天然唯一）；
//   - names 的 key 必须是已注册语言；含默认语言时必须与 name 一致（应用层校验）；
//   - 动作是叶子：任何节点都不能以动作节点为父；动作不能作根；
//   - parent 链不可成环、dir/menu 链深度 ≤ MaxDepth（动作不计层）；
//   - 翻译只涉及 name；path/component/icon/action_code 绝不翻译（docs §1.1）。
type Menu struct {
	ID         int64
	ParentID   int64
	Name       string            // 默认语言名（NOT NULL，兜底）
	Names      map[string]string // lang → 译文；树/展示不用，编辑回填用
	Type       Type
	Path       string
	Component  string
	Icon       string
	Sort       int
	Visible    bool
	Status     Status
	ActionCode string
	Remark     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// DisplayName 单一解析入口（docs §7：禁止各处自己写 fallback）。
// 回退顺序：请求 lang 翻译 → 主表 name（默认语言名，永远非空）。
func (m *Menu) DisplayName(lang i18n.Lang) string {
	if v, ok := m.Names[string(lang)]; ok && v != "" {
		return v
	}
	return m.Name
}

// Validate 保存前字段级校验（父结构校验在应用层：load 父节点/环/深度需要仓储）。
func (m *Menu) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return ErrNameRequired
	}
	if len(m.Name) > maxName {
		return ErrBadParams
	}
	switch m.Type {
	case TypeDir, TypeMenu, TypeAction:
	default:
		return ErrBadParams
	}
	if len(m.Path) > maxPath || len(m.Component) > maxPath ||
		len(m.Icon) > maxIcon || len(m.Remark) > maxRemark {
		return ErrBadParams
	}
	// v0.3：path / action_code 全部节点必填（同级唯一在 DB 生成列硬保证）
	if strings.TrimSpace(m.Path) == "" {
		return ErrBadParams // 请求地址不能为空（30034）
	}
	if strings.TrimSpace(m.ActionCode) == "" {
		return ErrBadParams // 权限点不能为空（30034）
	}
	if len(m.ActionCode) > maxCode {
		return ErrBadParams
	}
	// names：key 必须已注册（归一化在应用层做——ParseLang 后写入）；空/超长译文拒绝
	regs := i18n.Registered()
	for l, v := range m.Names {
		if _, ok := regs[i18n.Lang(l)]; !ok {
			return ErrLangUnsupported
		}
		if strings.TrimSpace(v) == "" || len(v) > maxName {
			return ErrBadParams
		}
	}
	return nil
}

// ---------- 仓储端口（由 infrastructure/db/dao 实现） ----------

// Repository sys_menu + sys_menu_i18n 的仓储端口。
type Repository interface {
	// Create 主表 + names 子表同事务新建；同级重复（v0.3: name/path/action_code）→ ErrSameLevelTaken。
	Create(ctx context.Context, m *Menu) error
	// ByID 主表 + names 全集；未命中 (nil, nil)。
	ByID(ctx context.Context, id int64) (*Menu, error)
	// Save 主表内容字段更新（name/path/component/icon/visible/status/action_code/remark）；
	// 同级重复 → ErrSameLevelTaken。
	Save(ctx context.Context, m *Menu) error
	// UpdateSort 仅更新排序（管理台“保存排序”批量调用）。
	UpdateSort(ctx context.Context, id int64, sort int) error
	// Move 结构变更：换父（父结构/环/深度校验由应用层完成）。
	Move(ctx context.Context, id, parentID int64) error
	// UpsertNames 覆盖所传语言（未传语言不动，docs §4 “names 可只传部分语言”）。
	UpsertNames(ctx context.Context, menuID int64, names map[string]string) error
	// Delete 软删主表 + 译文子表（HasChildren 由应用层先行拒绝）。
	Delete(ctx context.Context, id int64) error
	// HasChildren 是否存在未删除子节点。
	HasChildren(ctx context.Context, id int64) (bool, error)
	// ListAll 全量未删除节点（按 sort, id 排序；树组装/环检测入参；不含 names）。
	ListAll(ctx context.Context) ([]Menu, error)
	// NamesByIDs 批量取译文（树/列表补 names 用）。
	NamesByIDs(ctx context.Context, ids []int64) (map[int64]map[string]string, error)
}
