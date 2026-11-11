package i18n

import (
	"testing"

	pi18n "dugu-hr-saas-admin/pkg/i18n"
)

// 位映射必须覆盖全部已注册语言：新增 locales/*.json 而忘加 langBits 时，
// 该语言的录入会被存储层拒绝（30035），测试在这里先拦住（数据一致性守卫）。
func TestLangBitsCoverRegistered(t *testing.T) {
	for _, l := range pi18n.Languages() {
		if _, ok := LangBit(l); !ok {
			t.Fatalf("语言 %s 已注册但没有分配掩码位：请在 domain/i18n/bit.go langBits 追加下一个空闲位（只许追加，禁止重排）", l)
		}
	}
}

func TestFullMaskExcludesDefault(t *testing.T) {
	got := FullMask(pi18n.Default()) // 默认 zh-CN → zh-TW(2)|en-US(4)|ja-JP(8)|ko-KR(16)|es-ES(32)
	if got != 62 {
		t.Fatalf("FullMask(zh-CN) = %d, want 62", got)
	}
	if FullMask("zh-TW") != 61 { // 排除 zh-TW → zh-CN(1)|en-US(4)|ja-JP(8)|ko-KR(16)|es-ES(32)
		t.Fatalf("FullMask(zh-TW) = %d, want 61", FullMask("zh-TW"))
	}
}

func TestLangBitValues(t *testing.T) { // 位值落库即事实，改动必须过评审（docs §2.4）
	for l, want := range map[Lang]int64{
		"zh-CN": 1, "zh-TW": 2, "en-US": 4,
		"ja-JP": 8, "ko-KR": 16, "es-ES": 32, // v1.15 追加位（只许追加）
	} {
		if got, ok := LangBit(l); !ok || got != want {
			t.Fatalf("LangBit(%s) = %d,%v; want %d", l, got, ok, want)
		}
	}
}
