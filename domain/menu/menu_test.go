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
		Code:    "system:user:page",
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

func TestValidateRequiredForAll(t *testing.T) {
	// v0.3：path / action_code 全部节点必填（同级唯一在 DB 生成列硬保证）
	m := baseMenu()
	m.Path = ""
	if err := m.Validate(); err != ErrBadParams {
		t.Fatalf("path 为空应报 ErrBadParams, got %v", err)
	}
	m2 := baseMenu()
	m2.Code = ""
	if err := m2.Validate(); err != ErrBadParams {
		t.Fatalf("action_code 为空应报 ErrBadParams, got %v", err)
	}
	// 各类型统一规则（目录/动作同口径）
	for _, tt := range []Type{TypeDir, TypeMenu, TypeAction} {
		m3 := baseMenu()
		m3.Type = tt
		if tt == TypeAction {
			m3.Path = "system:user:remove" // 按钮 path 约定=action_code 同值
			m3.Code = "system:user:remove"
		}
		if err := m3.Validate(); err != nil {
			t.Fatalf("type=%d 合法节点不应报错: %v", tt, err)
		}
	}
}

func TestValidateLangUnsupported(t *testing.T) {
	m := baseMenu()
	m.Names = map[string]string{"fr-FR": "Utilisateur"} // 未注册语言
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

func TestValidateDirCodeOptional(t *testing.T) {
	// v0.4：目录 code 可空（应用层补 dir:<id>），menu/action 必填
	m := baseMenu()
	m.Type = TypeDir
	m.Path = "/org"
	m.Code = ""
	if err := m.Validate(); err != nil {
		t.Fatalf("目录空码应合法: %v", err)
	}
	// 动作保持必填
	m2 := baseMenu()
	m2.Type = TypeAction
	m2.Path = "system:user:remove"
	m2.Code = ""
	if err := m2.Validate(); err != ErrBadParams {
		t.Fatalf("动作空码应报 ErrBadParams, got %v", err)
	}
}
