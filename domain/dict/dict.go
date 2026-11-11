// Package dict —— 字典聚合（sys_dict_type / sys_dict_item）。
//
// docs《系统管理-字典(sys_dict)设计.md》§1/§4/§5。
// 标准DDD分层约束：零框架依赖（不 import gin/gorm/redis/config）。
// pkg/i18n 是纯标准库语言标签工具，被 domain 引用与 menu 包同一设计约定。
// label 的多语言存统一翻译表 sys_i18n_text（res_type='dict_item'、field='label'，
// 机制见《基础设施-数据文案多语言(i18n)设计.md》）；译文读写如何走统一表是
// 仓储实现细节，主表 Repository 端口定义在此，由 infrastructure/db/dao 实现（DIP）。
//
// 两条最关键的不变式（docs §1.4 / §7）：
//   - value 永远单值：业务判断、落库、接口传参只许用 value；语言只影响 label；
//   - sort / status 不分语言：各语言下拉顺序、启停状态必须一致。
package dict

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"dugu-hr-saas-admin/pkg/i18n"
)

// ---------- 聚合内错误（领域语言；业务码见 pkg/code 30041~30046） ----------

var (
	// ErrTypeNotFound 字典类型不存在（含软删）→ 30041。
	ErrTypeNotFound = errors.New("dict: type not found")
	// ErrItemNotFound 字典项不存在（含软删）→ 30042。
	ErrItemNotFound = errors.New("dict: item not found")
	// ErrTypeCodeTaken 类型编码重复（uk_dict_type，全局唯一）→ 30043。
	ErrTypeCodeTaken = errors.New("dict: type code taken")
	// ErrValueTaken 同类型下取值重复（uk_dict_item(type_id,value,deleted_at)）→ 30044。
	ErrValueTaken = errors.New("dict: value taken")
	// ErrTypeHasItems 类型下存在字典项，禁止删除（不做级联软删，docs §5）→ 30045。
	ErrTypeHasItems = errors.New("dict: type has items")
	// ErrBadParams 字典参数不合法（code/value/label 格式、长度、空值等）→ 30046。
	ErrBadParams = errors.New("dict: bad params")
	// ErrLangUnsupported labels 的 key 不是已注册语言 → 30035（与菜单共用，docs §4）。
	ErrLangUnsupported = errors.New("dict: lang unsupported")
)

// ---------- 类型（不变式，docs §1/§2） ----------

// Status 1启用 0停用（停用 ≠ 删除：停用项不下发但历史数据仍引用）。
type Status int8

const (
	StatusOn  Status = 1
	StatusOff Status = 0
)

// 字段长度上限（与 DDL 一致）。
const (
	maxCode   = 64
	maxName   = 64
	maxValue  = 64
	maxLabel  = 64
	maxRemark = 255
)

// codeRe 类型编码：小写蛇形 ^[a-z][a-z0-9_]{1,63}$（docs §5）。
// code 是前端取值的唯一入口，一旦被引用就不该改（§1.3：改 code 等于静默打断调用方）。
var codeRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

// valueRe 取值字符集 [A-Za-z0-9_.:-]：禁空格与斜杠，避免进 URL / 作为 key 前缀时出事（docs §5）。
var valueRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

// ---------- 聚合根 ----------

// Type 字典类型。code 全局唯一（前端唯一入参）；name 只在后台列表展示，本期单语不翻译。
type Type struct {
	ID        int64
	Code      string
	Name      string
	Status    Status
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate 保存前字段级校验（docs §5）。
func (t *Type) Validate() error {
	if !codeRe.MatchString(t.Code) {
		return ErrBadParams
	}
	if strings.TrimSpace(t.Name) == "" || len(t.Name) > maxName {
		return ErrBadParams
	}
	if t.Status != StatusOn && t.Status != StatusOff {
		return ErrBadParams
	}
	if len(t.Remark) > maxRemark {
		return ErrBadParams
	}
	return nil
}

// Item 字典项。value 是业务落库的单值；label 是默认语言显示名（兜底，NOT NULL）；
// Labels（lang→译文）仅编辑回填用，展示走 DisplayLabel，落库到统一翻译表。
type Item struct {
	ID        int64
	TypeID    int64
	Value     string
	Label     string
	Labels    map[string]string // lang → label；仅编辑与回填用，展示不用
	Sort      int
	Status    Status
	Remark    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DisplayLabel 单一解析入口（docs §5：禁止各处自己写 fallback）。
// 回退顺序：请求 lang 的译文 → Label（主表列 NOT NULL，永远非空）。
func (i *Item) DisplayLabel(lang i18n.Lang) string {
	if v, ok := i.Labels[string(lang)]; ok && v != "" {
		return v
	}
	return i.Label
}

// Validate 保存前字段级校验（docs §5）：value/label 非空限长；labels 的 key
// 必须已注册（否则 30035）、译文非空 ≤64、提交语言数 ≤ 已注册语言数（防脏 key）。
func (i *Item) Validate() error {
	if !valueRe.MatchString(i.Value) || len(i.Value) > maxValue {
		return ErrBadParams
	}
	if strings.TrimSpace(i.Label) == "" || len(i.Label) > maxLabel {
		return ErrBadParams
	}
	if i.Status != StatusOn && i.Status != StatusOff {
		return ErrBadParams
	}
	if i.Sort < 0 {
		return ErrBadParams
	}
	if len(i.Remark) > maxRemark {
		return ErrBadParams
	}
	regs := i18n.Registered()
	for l, v := range i.Labels {
		if _, ok := regs[i18n.Lang(l)]; !ok {
			return ErrLangUnsupported
		}
		if strings.TrimSpace(v) == "" || len(v) > maxLabel {
			return ErrBadParams
		}
	}
	if len(i.Labels) > len(regs) { // 提交语言数 ≤ 已注册语言数（防脏 key 撑爆，docs §5）
		return ErrBadParams
	}
	return nil
}

// ---------- 仓储端口（由 infrastructure/db/dao 实现） ----------

// ListTypesQuery 类型列表查询（docs §3：code / status 过滤 + page/pageSize 分页）。
type ListTypesQuery struct {
	Code   string // 非空 = 编码模糊过滤（后台搜索）
	Status *Status
	Page   int
	Size   int
}

// TypeRepository sys_dict_type 仓储端口。
type TypeRepository interface {
	// Create 新建；code 重复（uk_dict_type）→ ErrTypeCodeTaken。
	Create(ctx context.Context, t *Type) error
	// ByID 未命中 (nil, nil)。
	ByID(ctx context.Context, id int64) (*Type, error)
	// ByCode 未命中 (nil, nil)（应用层按 typeCode 定位类型用，docs §3 取舍 2）。
	ByCode(ctx context.Context, code string) (*Type, error)
	// Save 内容字段更新（code/name/status/remark）；code 重复 → ErrTypeCodeTaken。
	Save(ctx context.Context, t *Type) error
	// Delete 软删（项数>0 由应用层先行拒绝，docs §5 不做级联）。
	Delete(ctx context.Context, id int64) error
	// List 分页列表（code 模糊 / status 过滤；按 code 升序，稳定顺序）。
	List(ctx context.Context, q ListTypesQuery) (items []Type, total int64, err error)
	// CountItems 类型下未删除项数（删除前置校验，>0 → ErrTypeHasItems）。
	CountItems(ctx context.Context, typeID int64) (int64, error)
}

// ItemRepository sys_dict_item 仓储端口（label 译文存统一翻译表，实现细节在 infra）。
type ItemRepository interface {
	// Create 新建 + 统一表空译文行 + labels 非默认语言部分同事务写入；
	// value 同类型重复（uk_dict_item）→ ErrValueTaken。
	Create(ctx context.Context, i *Item) error
	// ByID 主表 + labels 全集（编辑页回填）；未命中 (nil, nil)。
	ByID(ctx context.Context, id int64) (*Item, error)
	// Save 主表内容字段更新（label/value/sort/status/remark）；value 重复 → ErrValueTaken。
	Save(ctx context.Context, i *Item) error
	// Delete 软删主表 + 统一翻译表对应译文行（同一事务，docs 基础设施-i18n设计 §5）。
	Delete(ctx context.Context, id int64) error
	// ListByType 类型下项列表：sort 升序同 sort 按 id（稳定顺序，docs §2）；
	// withDisabled=false 只返回启用项；lang 非空时按该语言解析展示 label（回退主表列）。
	ListByType(ctx context.Context, typeID int64, withDisabled bool, lang i18n.Lang) ([]Item, error)
	// UpsertLabels 覆盖所传语言（未传语言不动，docs §3 取舍 4）；空串 = 删除该语言译文。
	UpsertLabels(ctx context.Context, itemID int64, labels map[string]string) error
}
