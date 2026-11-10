// Package i18n —— 数据文案统一翻译表的领域模型（docs《基础设施-数据文案多语言(i18n)设计.md》）。
//
// 定位：所有"运营维护的展示文案"（菜单 name、字典项 label、将来的部门名/岗位名…）
// 共用一张 `sys_i18n_text` 通用表。本包定义：
//   - 资源类型与可翻译字段的白名单（**不登记 = 禁止翻译**，堵死翻译标识字段的入口）；
//   - 统一读写端口 Repo（infrastructure/db/dao 实现，DIP）；
//   - 三个领域错误 → 业务码 30035/30038/30039（pkg/code 三处同步）。
//
// 零框架依赖；语言标签工具复用 pkg/i18n（语言注册表唯一真相，避免两处 truth）。
package i18n

import (
	"context"
	"errors"
	"strings"

	pi18n "dugu-hr-saas-admin/pkg/i18n"
)

// Lang 语言标签（与 pkg/i18n 同型，端口签名直接复用其常量/解析）。
type Lang = pi18n.Lang

// Resource 资源类型（sys_i18n_text.res_type 的取值域）。
type Resource string

const (
	ResMenu     Resource = "menu"      // 菜单名（原 sys_menu_i18n）
	ResDictItem Resource = "dict_item" // 字典项 label（字典设计 §7）
	// 新增资源在此追加：同时登记 translatableFields 可翻字段。
)

// translatableFields 可翻译字段白名单（值为该字段的业务长度上限）。
// **只登记允许翻译的字段**：path/code/value/icon/component/sort/status 这类
// 标识字段根本不出现——不是"约定不翻"，而是"登记了就会被拒"（30038）。
// 翻错对象是真实事故：path 翻了路由 404、code 翻了权限匹配失效、字典 value
// 翻了历史数据按语言分裂。文档 §3。
var translatableFields = map[Resource]map[string]int{
	ResMenu:     {"name": 64},
	ResDictItem: {"label": 64},
}

// ---------- 领域错误（application 层映射业务码） ----------

var (
	// ErrLangUnsupported 语言不在 i18n.Registered() 内 → 30035（复用菜单既有码）。
	ErrLangUnsupported = errors.New("i18n: lang not registered")
	// ErrTargetUnregistered 资源类型或字段未登记（白名单外）→ 30038。
	ErrTargetUnregistered = errors.New("i18n: resource/field not registered")
	// ErrContentInvalid 译文超长或含非法字符（控制字符、\x00）→ 30039。
	ErrContentInvalid = errors.New("i18n: content invalid")
)

// ValidateTarget 一次完整校验：资源已登记、字段在白名单内、语言已注册、
// 非空译文不超长且不含控制字符。content 为空串不校验内容（空串 = 删除该语言的
// 保存语义，见 Repo.Save）。任何一项失败返回对应领域错误（30038/30035/30039）。
func ValidateTarget(r Resource, field, lang, content string) error {
	caps, ok := translatableFields[r]
	if !ok {
		return ErrTargetUnregistered
	}
	maxLen, ok := caps[field]
	if !ok {
		return ErrTargetUnregistered
	}
	if _, ok := pi18n.ParseLang(lang); !ok {
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

// FieldCap 返回资源某字段的长度上限（未登记 → 0）。领域校验用。
func FieldCap(r Resource, field string) int {
	if caps, ok := translatableFields[r]; ok {
		return caps[field]
	}
	return 0
}

// Fields 返回资源白名单内全部可翻字段（GetMany fields 为空时取全量）。
func Fields(r Resource) []string {
	caps, ok := translatableFields[r]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(caps))
	for f := range caps {
		out = append(out, f)
	}
	return out
}

// Repo 统一译文读写端口（infrastructure/db/dao 实现；MySQL 表 sys_i18n_text）。
//
// 三条硬约束（docs §5，由实现与本签名共同保证）：
//   - Get 是列表页**唯一**读法——批量取，禁止调用方 for 循环逐条查询（N+1）；
//   - Save 覆盖"传入的语言"，未传语言原样保留；空串 = 软删该语言；
//   - DeleteAll 必须与资源主表删除**同一事务**（事务内场景走 dao 内部 saveTx/deleteAllTx）。
type Repo interface {
	// Get 列表页批量取某字段某语言的译文；命不中的 id 不出现在 map，调用方回退主表列。
	Get(ctx context.Context, r Resource, ids []int64, field string, lang Lang) (map[int64]string, error)

	// GetMany 详情/编辑页用：id → field → lang → 译文。
	// fields/langs 任一为 nil/空 = 该维度取白名单全量/全部已注册语言。
	GetMany(ctx context.Context, r Resource, ids []int64, fields []string, langs []Lang) (map[int64]map[string]map[Lang]string, error)

	// Save 只覆盖传入语言。texts 结构：field → lang → 译文（空串 = 删除该语言）。
	Save(ctx context.Context, r Resource, id int64, texts map[string]map[Lang]string) error

	// DeleteAll 软删该资源全部译文。**资源删除时必须与主表同事务**调用。
	DeleteAll(ctx context.Context, r Resource, id int64) error
}
