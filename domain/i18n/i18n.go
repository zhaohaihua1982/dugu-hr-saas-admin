// Package i18n —— 数据文案统一翻译表的领域模型（docs《基础设施-数据文案多语言(i18n)设计.md》）。
//
// 定位：所有"运营维护的展示文案"（菜单 name、字典项 label、将来的部门名/岗位名…）
// 共用一张 `sys_i18n_text` 通用表。本包定义：
//   - 资源注册表 Resource → Spec{Label, Fields 白名单, Resolver}（§7.3）：
//     **不登记 = 禁止翻译**，未登记 → 30038；Resolver 校验目标资源真实存在 → 30040；
//   - 统一读写端口 Repo 与资源解析端口 Resolver（infrastructure/db/dao 实现，DIP）；
//   - 领域错误 → 业务码 30035/30038/30039/30040（pkg/code 五处同步）。
//
// 零框架依赖；语言标签工具复用 pkg/i18n（语言注册表唯一真相）。
package i18n

import (
	"context"
	"errors"
	"strings"
	"time"

	pi18n "dugu-hr-saas-admin/pkg/i18n"
)

// Lang 语言标签（与 pkg/i18n 同型，端口签名直接复用其常量/解析）。
type Lang = pi18n.Lang

// Resource 资源类型（sys_i18n_text.res_type 的取值域）。
type Resource string

const (
	ResMenu     Resource = "menu"
	ResDictItem Resource = "dict_item" // 字典模块落地时注册（docs 字典设计 §7）
	// 新增资源在此追加，并在 main 装配时 Register（docs §10 六件事）。
)

// ---------- 资源注册表（§7.3：白名单并带行为） ----------

// ResItem Resolver.Snapshot 的行：资源的可读标识与默认语言字段值（翻译页"原文"列）。
type ResItem struct {
	ID    int64
	Key   string // 人类可读标识：菜单 name / 字典项 value（列表与审计展示用）
	Value string // 默认语言下的字段值
}

// Resolver 资源行为端口——由各资源在 infrastructure 实现并 Register，
// 不生效则该资源无法进入译文管理页（docs §7.3 / 验收 17）。
type Resolver interface {
	// Exists 写入前校验目标资源存在且未被逻辑删除（false → 30040）。
	Exists(ctx context.Context, id int64) (bool, error)
	// Snapshot 批量取 id → ResItem：一次 IN 查询，禁止循环（§5 N+1 红线）。
	Snapshot(ctx context.Context, ids []int64, field string) (map[int64]ResItem, error)
	// ListIDs 常规分页取主表 id（offset 页码语义，v1.12 与 /texts 同构）。
	// 只取 id，不取业务字段——通用层不该知道业务的筛选/排序规则。
	ListIDs(ctx context.Context, offset, limit int) ([]int64, error)
	// CountLive 该资源类型活资源总数（分页 total 估算用；v1.12）。
	CountLive(ctx context.Context) (int64, error)
	// PermWrite 该资源的写权限码（如 "menu:update"，docs §7.8）。
	// 现状仅声明：按钮级鉴权中间件未上线前接口仍只要求登录态。
	PermWrite() string
}

// Spec 资源注册项。
type Spec struct {
	Label    string         // 中文名（审计与前端页签，docs §7.3）
	Fields   map[string]int // 字段 → 业务长度上限（原 translatableFields）
	Resolver Resolver
}

// registry 资源注册表：main 装配期 Register（避免 domain 反向依赖 infrastructure）。
var registry = map[Resource]Spec{}

// Register 注册资源（幂等覆盖；语法错误由启动期统一报出）。
func Register(r Resource, s Spec) { registry[r] = s }

// SpecOf 查注册项（未注册 → ok=false，调用方统一 30038）。
func SpecOf(r Resource) (Spec, bool) {
	s, ok := registry[r]
	return s, ok
}

// HasField (资源, 字段) 是否在白名单内。
func HasField(r Resource, field string) bool {
	s, ok := registry[r]
	if !ok {
		return false
	}
	_, ok = s.Fields[field]
	return ok
}

// FieldCap 返回资源某字段的长度上限（未登记 → 0）。
func FieldCap(r Resource, field string) int {
	s, ok := registry[r]
	if !ok {
		return 0
	}
	return s.Fields[field]
}

// Fields 返回资源白名单内全部可翻字段（GetMany fields 为空时取全量）。
func Fields(r Resource) []string {
	s, ok := registry[r]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(s.Fields))
	for f := range s.Fields {
		out = append(out, f)
	}
	return out
}

// ---------- 领域错误（application 层映射业务码） ----------

var (
	// ErrLangUnsupported 语言不在 i18n.Registered() 内 → 30035（复用菜单既有码）。
	ErrLangUnsupported = errors.New("i18n: lang not registered")
	// ErrTargetUnregistered 资源类型/字段未登记，或资源未挂 Resolver → 30038。
	ErrTargetUnregistered = errors.New("i18n: resource/field not registered")
	// ErrContentInvalid 译文超长或含非法字符（控制字符、\x00）→ 30039。
	ErrContentInvalid = errors.New("i18n: content invalid")
	// ErrResNotFound 译文目标资源不存在（resType 合法但主表查无此行/已软删）→ 30040。
	ErrResNotFound = errors.New("i18n: target resource not found")
	// ErrDefaultLang 默认语言不进译文表（读走主表列）→ 400 InvalidParams，msg 走
	// 语言包键 "i18n.defaultLang"（"默认语言请通过资源编辑修改"，docs §7.4）。
	ErrDefaultLang = errors.New("i18n: default lang not allowed here")
	// ErrBadReq 请求参数不合法（res_id 非法/为空等）→ 400 InvalidParams。
	ErrBadReq = errors.New("i18n: bad request")
)

// ValidateTarget 一次写入前校验：资源已注册、字段在白名单内、语言已注册且
// **不是默认语言**（§7.4，default 检查处留 allowDefault 开关给内部通道）、
// 非空译文不超长且不含控制字符。content 为空串 = 删除该语言的保存语义，不校验内容。
func ValidateTarget(r Resource, field, lang, content string) error {
	s, ok := registry[r]
	if !ok {
		return ErrTargetUnregistered
	}
	maxLen, ok := s.Fields[field]
	if !ok {
		return ErrTargetUnregistered
	}
	if _, ok := pi18n.ParseLang(lang); !ok {
		return ErrLangUnsupported
	}
	// 已注册但未分配掩码位的语言同样拒绝（§2.4）：lang_mask 无法表达它的录入状态，
	// complete 会永远算成"缺失"。新增语言必须先在 bit.go langBits 追加位。
	if _, ok := LangBit(pi18n.Lang(lang)); !ok {
		return ErrLangUnsupported
	}
	if content == "" {
		return nil
	}
	if strings.ContainsRune(content, '\x00') {
		return ErrContentInvalid
	}
	for _, c := range content {
		if c < 0x20 && c != '\t' {
			return ErrContentInvalid
		}
	}
	if len(content) > maxLen {
		return ErrContentInvalid
	}
	return nil
}

// Repo 统一译文读写端口（infrastructure/db/dao 实现；MySQL 表 sys_i18n_text）。
//
// 三条硬约束（docs §5，由实现与本签名共同保证）：
//   - Get 是列表页**唯一**读法——批量取，禁止调用方 for 循环逐条查询（N+1）；
//   - Save 覆盖"传入的语言"，未传语言原样保留；空串 = 软删该语言；
//     默认语言不进本表（§7.4）：调用方不应传 default 键，传了由上层拒绝；
//   - DeleteAll 必须与资源主表删除**同一事务**。
type Repo interface {
	// Get 列表页批量取某字段某语言的译文；命不中的 id 不出现在 map，调用方回退主表列。
	Get(ctx context.Context, r Resource, ids []int64, field string, lang Lang) (map[int64]string, error)

	// GetMany 详情/编辑页用：id → field → lang → 译文。
	// fields/langs 任一为 nil/空 = 该维度取白名单全量/全部已注册语言。
	GetMany(ctx context.Context, r Resource, ids []int64, fields []string, langs []Lang) (map[int64]map[string]map[Lang]string, error)

	// Save 只覆盖传入语言。texts 结构：field → lang → 译文（空串 = 删除该语言）。
	Save(ctx context.Context, r Resource, id int64, texts map[string]map[Lang]string) error

	// ListRows 译文总览分页（**非聚合 + 正常分页**，v1.6/v1.7 用户裁定）：直接投影
	// sys_i18n_text 合并粒度行，不做主表 join（key/source 由前端按 resId 向资源接口
	// 另取）。page/page_size 为常规页码分页（page≤0→1、size≤0→10、size>100→100，
	// 与 sys-users 列表同一 normalizePage 惯例）；返回 total 供前端翻页器。
	// field 空 = 白名单全字段（行按 res_id,field 排序）；field 指定 = 按 res_id 排序。
	ListRows(ctx context.Context, r Resource, field string, page, pageSize int) (rows []Row, total int64, err error)

	// BatchDelete 批量软删（§7.7 三档第 2 档）：**单事务全成功或全失败**，
	// 返回实际推进的行数；删不存在的译文幂等成功、计 0。
	BatchDelete(ctx context.Context, keys []DelKey) (int64, error)

	// GetRows 批量取原始行（v1.12 pending 用：缺译清单对"已有部分译文"的行
	// 原样透出 texts JSON 串）。命不中的 id 不出现在结果里。
	GetRows(ctx context.Context, r Resource, ids []int64) ([]Row, error)

	// CountWithLang 已录入某语言的活行数（pending total 用：
	// 缺译总数 = CountLive - CountWithLang，孤儿行按已译计的轻微误差可接受）。
	CountWithLang(ctx context.Context, r Resource, field string, lang Lang) (int64, error)

	// DeleteAll 软删该资源全部译文（资源删除场景必须走同事务的内部信道）。
	DeleteAll(ctx context.Context, r Resource, id int64) error
}

// Row 译文总览行（**DB 原样投影，v1.9 用户裁定：直接读数据库，不做任何加工**）：
// texts = 库里存储的 JSON 串原样（不解码、不补缺译键、不剔默认语言）；
// lang_mask/complete/deleted_at 原样数值；时间原样透传。
// 唯一保留的两个过滤：deleted_at=0（墓碑不进列表）与分页——那是查询范围，不是数据加工。
type Row struct {
	ID        int64
	ResType   string
	ResID     int64
	Field     string
	Texts     string // DB 原样 JSON 串：{"lang":"译文",...}（键序为落库序）
	LangMask  int64
	Complete  int8
	DeletedAt int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DelKey 批量删除的操作对象：四元组（§7.1——CRUD 主键不是行 id）。
type DelKey struct {
	Resource Resource
	ID       int64
	Field    string
	Lang     Lang
}
