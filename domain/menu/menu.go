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
	// ErrActionCodeTaken 动作编码已被占用（uk_action 全局唯一）→ 30033。
	ErrActionCodeTaken = errors.New("menu: action_code taken")
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

// MaxDepth 任意链路最大层级（防止恶意构造超深树；docs 未定死，登记为实现约束）。
const MaxDepth = 8

// 字段长度上限（与 DDL 一致）。
const (
	maxName   = 64
	maxPath   = 128
	maxIcon   = 64
	maxCode   = 64
	maxRemark = 255
)

// ---------- 聚合根 ----------

// Menu 菜单聚合根。不变式：
//   - 主表 name 非空（默认语言名，永远有显示兜底，docs §1.1）；
//   - names 的 key 必须是已注册语言；含默认语言时必须与 name 一致（应用层校验）；
//   - type=3 动作：action_code 必填（全局唯一 uk_action）；其余类型 action_code 必空
//     （非动作不占全局唯一码，防后续动作建不进来）；
//   - 动作是叶子：任何节点都不能以动作节点为父；
//   - parent 链不可成环、深度 ≤ MaxDepth；
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
	if m.Type == TypeAction {
		if strings.TrimSpace(m.ActionCode) == "" {
			return ErrBadParams // 动作必须带编码（30034）
		}
	} else if m.ActionCode != "" {
		return ErrBadParams // 非动作不占全局唯一码
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
	// Create 主表 + names 子表同事务新建；uk_action 冲突 → ErrActionCodeTaken。
	Create(ctx context.Context, m *Menu) error
	// ByID 主表 + names 全集；未命中 (nil, nil)。
	ByID(ctx context.Context, id int64) (*Menu, error)
	// Save 主表内容字段更新（name/path/component/icon/visible/status/action_code/remark）；
	// uk_action 冲突 → ErrActionCodeTaken。
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
