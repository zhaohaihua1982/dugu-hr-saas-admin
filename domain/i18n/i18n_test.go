package i18n

import (
	"strings"
	"testing"
)

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
		{"字典字段名错", ResDictItem, "name", "en-US", "D", ErrTargetUnregistered},
		{"语言未注册", ResMenu, "name", "ja-JP", "D", ErrLangUnsupported},
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
