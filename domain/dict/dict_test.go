package dict

import (
	"errors"
	"testing"

	"dugu-hr-saas-admin/pkg/i18n"
)

func TestTypeValidate(t *testing.T) {
	ok := &Type{Code: "import_type", Name: "导入业务域", Status: StatusOn}
	if err := ok.Validate(); err != nil {
		t.Fatalf("合法类型不应报错: %v", err)
	}
	bad := []*Type{
		{Code: "", Name: "x", Status: StatusOn},                         // code 空
		{Code: "Import", Name: "x", Status: StatusOn},                   // 大写开头
		{Code: "a_b-c", Name: "x", Status: StatusOn},                    // 连字符不在字符集
		{Code: "ab", Name: "", Status: StatusOn},                        // name 空
		{Code: "ab", Name: "x", Status: StatusOn, Remark: makeStr(256)}, // remark 超长
	}
	for i, b := range bad {
		if err := b.Validate(); !errors.Is(err, ErrBadParams) {
			t.Fatalf("case %d 应报 ErrBadParams, got %v", i, err)
		}
	}
}

func TestItemValidate(t *testing.T) {
	ok := &Item{Value: "employee", Label: "员工花名册", Status: StatusOn}
	if err := ok.Validate(); err != nil {
		t.Fatalf("合法项不应报错: %v", err)
	}
	// labels：key 必须已注册 → 30035
	l := &Item{Value: "employee", Label: "员工花名册", Status: StatusOn,
		Labels: map[string]string{"fr-FR": "x"}}
	if err := l.Validate(); err != ErrLangUnsupported {
		t.Fatalf("未注册语言应报 ErrLangUnsupported, got %v", err)
	}
	// labels：空译文 → 30046（BadField 包装，errors.Is 仍命中）
	l2 := &Item{Value: "employee", Label: "员工花名册", Status: StatusOn,
		Labels: map[string]string{"en-US": "  "}}
	if err := l2.Validate(); !errors.Is(err, ErrBadParams) {
		t.Fatalf("空译文应报 ErrBadParams, got %v", err)
	}
	bad := []*Item{
		{Value: "", Label: "x", Status: StatusOn},    // value 空
		{Value: "a b", Label: "x", Status: StatusOn}, // value 禁空格
		{Value: "a/b", Label: "x", Status: StatusOn}, // value 禁斜杠
		{Value: "a", Label: "", Status: StatusOn},    // label 空（NOT NULL 兜底）
		{Value: "a", Label: "x", Status: 9},          // status 非法
	}
	for i, b := range bad {
		if err := b.Validate(); !errors.Is(err, ErrBadParams) {
			t.Fatalf("case %d 应报 ErrBadParams, got %v", i, err)
		}
	}
}

func TestDisplayLabelFallback(t *testing.T) {
	i := &Item{Label: "员工花名册", Labels: map[string]string{"en-US": "Employee Roster"}}
	if got := i.DisplayLabel("en-US"); got != "Employee Roster" {
		t.Fatalf("DisplayLabel(en-US) = %q, want Employee Roster", got)
	}
	if got := i.DisplayLabel("zh-TW"); got != "员工花名册" { // 缺译回退主表 label（永不空串）
		t.Fatalf("缺译应回退主表 label, got %q", got)
	}
}

func makeStr(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

// 守卫：labels 提交语言数上限随语言注册表派生（防脏 key 撑爆）。
func TestLabelsCountCap(t *testing.T) {
	labels := map[string]string{}
	for i, l := range i18n.Languages() {
		_ = i
		labels[string(l)] = "x"
	}
	i := &Item{Value: "a", Label: "x", Status: StatusOn, Labels: labels}
	if err := i.Validate(); err != nil {
		t.Fatalf("全注册语言提交应通过: %v", err)
	}
	labels["extra"] = "y"
	i.Labels = labels
	if err := i.Validate(); err != ErrLangUnsupported {
		t.Fatalf("超注册语言数应报 ErrLangUnsupported, got %v", err)
	}
}
