// bit.go —— 语言位掩码：sys_i18n_text.lang_mask / complete 的编码
// （docs《基础设施-数据文案多语言(i18n)设计.md》§2.4，v1.5）。
//
// 合并粒度下一行 = 资源 × 字段，全部语言进 texts JSON；lang_mask 用位图记录
// "哪些语言已录入"，让缺译类查询能用 `WHERE complete = 0` 直接走索引，
// 而不必解析 JSON 逐行比对。
package i18n

import (
	pi18n "dugu-hr-saas-admin/pkg/i18n"
)

// langBits 语言 → 掩码位。**固定分配、只许追加、严禁重排**：位序一旦落库即成事实，
// 在中间插一门语言会让全部历史行的位含义整体错位（等于数据损坏）。
// 新增语言的正确姿势：locales/ 注册语言包 + 在此追加下一个空闲位 +
// 跑 TestLangBitsCoverRegistered 守卫（漏配时测试先红，而不是线上录入被拒）。
var langBits = map[Lang]int64{
	Lang("zh-CN"): 1 << 0, // 1 —— 默认语言位：本表不存默认语言（§7.4），恒为 0
	Lang("zh-TW"): 1 << 1, // 2
	Lang("en-US"): 1 << 2, // 4
}

// LangBit 语言对应的掩码位。ok=false = 该语言已注册但没分配位（新增语言忘登记），
// 存储层必须拒绝录入——否则该语言的录入状态无法被 lang_mask 表达，
// complete 会永远算成"缺失"。
func LangBit(l Lang) (int64, bool) {
	b, ok := langBits[l]
	return b, ok
}

// FullMask 已注册语言（排除 exclude——调用方传默认语言）的位全集。
// complete = (lang_mask & FullMask(默认语言)) == FullMask(默认语言)。
// 默认语言切换后由配置变更流程对全表重算 complete（§2.4）。
func FullMask(exclude Lang) int64 {
	var m int64
	for _, l := range pi18n.Languages() {
		if l == exclude {
			continue
		}
		if b, ok := langBits[l]; ok {
			m |= b
		}
	}
	return m
}

// Bits 语言→位的完整枚举（只读拷贝；GET /i18n/meta 透出给前端，
// 前端用 langMask & bit 反解"已录入语言"，避免前端再硬编码一份位表）。
func Bits() map[Lang]int64 {
	out := make(map[Lang]int64, len(langBits))
	for l, b := range langBits {
		out[l] = b
	}
	return out
}
