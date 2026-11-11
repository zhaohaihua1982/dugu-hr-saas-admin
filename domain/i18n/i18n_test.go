package i18n

import (
	"strings"
	"testing"
)

// testSpec 注册最小测试规格（main 装配期才注册 menu；单测自给自足）。
func init() {
	SpecsForTest = map[Resource]Spec{
		ResMenu: {Label: "菜单", Fields: map[string]int{"name": 64}},
	}
	for r, s := range SpecsForTest {
		Register(r, s)
	}
}

// SpecsForTest 供 init 注册（无 Resolver：ValidateTarget 不需要 Resolver）。
var SpecsForTest map[Resource]Spec

func TestValidateTarget(t *testing.T) {
	cases := []struct {
		name             string
		r                Resource
		f, lang, content string
		want             error
	}{
		{"合法", ResMenu, "name", "en-US", "Users", nil},
		{"空内容=删除语义不校验", ResMenu, "name", "en-US", "", nil},
		{"资源未登记", Resource("dept"), "name", "en-US", "D", ErrTargetUnregistered},
		{"字段未登记(标识字段直接拒)", ResMenu, "path", "en-US", "/x", ErrTargetUnregistered},
		{"字段未登记(其他资源的字段名)", ResMenu, "label", "en-US", "D", ErrTargetUnregistered},
		{"语言未注册", ResMenu, "name", "fr-FR", "D", ErrLangUnsupported},
		{"超长(64+1)", ResMenu, "name", "en-US", strings.Repeat("a", 65), ErrContentInvalid},
		{"正好64", ResMenu, "name", "en-US", strings.Repeat("a", 64), nil},
		{"含NUL", ResMenu, "name", "en-US", "a\x00b", ErrContentInvalid},
	}
	for _, c := range cases {
		if got := ValidateTarget(c.r, c.f, c.lang, c.content); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestSpecOfUnregistered(t *testing.T) { // 验收 17：未实现 Resolver 的资源 → 30038，不 panic
	if _, ok := SpecOf(ResDictItem); ok {
		t.Fatal("ResDictItem 未落地字典模块前不应已注册")
	}
	if HasField(ResDictItem, "label") {
		t.Fatal("未注册资源的字段不应通过白名单")
	}
}
