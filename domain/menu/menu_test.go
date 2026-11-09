// 菜单领域单测：字段校验、展示名回退链、动作编码对偶规则。
package menu

import (
	"testing"

	"dugu-hr-saas-admin/pkg/i18n"
)

func baseMenu() *Menu {
	return &Menu{
		Name:    "用户管理",
		Type:    TypeMenu,
		Path:    "/system/user",
		Visible: true,
		Status:  StatusOn,
	}
}

func TestValidateOK(t *testing.T) {
	m := baseMenu()
	m.Names = map[string]string{"en-US": "Users"}
	if err := m.Validate(); err != nil {
		t.Fatalf("合法节点不应报错: %v", err)
	}
}

func TestValidateNameRequired(t *testing.T) {
	m := baseMenu()
	m.Name = " "
	if err := m.Validate(); err != ErrNameRequired {
		t.Fatalf("name 为空应报 ErrNameRequired, got %v", err)
	}
}

func TestValidateActionRules(t *testing.T) {
	// 动作必须带 code
	m := baseMenu()
	m.Type = TypeAction
	m.Path = ""
	if err := m.Validate(); err != ErrBadParams {
		t.Fatalf("动作缺编码应报 ErrBadParams, got %v", err)
	}
	m.ActionCode = "system:user:remove"
	if err := m.Validate(); err != nil {
		t.Fatalf("动作带编码应合法: %v", err)
	}
	// 非动作不允许占用全局唯一码
	m2 := baseMenu()
	m2.ActionCode = "system:user:view"
	if err := m2.Validate(); err != ErrBadParams {
		t.Fatalf("非动作带编码应报 ErrBadParams, got %v", err)
	}
}

func TestValidateLangUnsupported(t *testing.T) {
	m := baseMenu()
	m.Names = map[string]string{"ja-JP": "ユーザー"} // 未注册语言
	if err := m.Validate(); err != ErrLangUnsupported {
		t.Fatalf("未注册语言应报 ErrLangUnsupported, got %v", err)
	}
}

func TestDisplayNameFallback(t *testing.T) {
	m := baseMenu()
	m.Names = map[string]string{"zh-CN": "用户管理"}
	// en-US 未翻译 → 回退主表 name（永不空白，docs §1.1）
	if got := m.DisplayName(i18n.LangEN); got != "用户管理" {
		t.Fatalf("回退链失败: %q", got)
	}
	if got := m.DisplayName(i18n.LangZH); got != "用户管理" {
		t.Fatalf("命中译文失败: %q", got)
	}
}
