// 多语言解析与回退链测试。
package i18n

import (
	"os"
	"testing"
)

func TestRegisteredLanguages(t *testing.T) {
	langs := Languages()
	if len(langs) < 2 {
		t.Fatalf("至少应注册 zh-CN/en-US 两个语言包，实际 %v", langs)
	}
	if !Supported("en_us") || !Supported("EN-US") {
		t.Fatal("大小写/下划线写法应归一化命中")
	}
	if Supported("ja-JP") {
		t.Fatal("未注册语言不应命中")
	}
}

func TestMsgBasic(t *testing.T) {
	if m := Msg(LangZH, 30010); m == "" {
		t.Fatal("zh-CN 应有 30010 文案")
	}
	if m := Msg(LangEN, 30010); m != "Incorrect account or password" {
		t.Fatalf("en-US 30010 文案错误: %q", m)
	}
}

func TestMsgWrongCodeFallback(t *testing.T) {
	// 未知码回退同包 "500"，文案非空
	if m := Msg(LangZH, 99999); m == "" || m == "30010" {
		t.Fatalf("未知码应回退 500 文案: %q", m)
	}
}

func TestSetDefaultLangAndEnv(t *testing.T) {
	t.Cleanup(func() { Reset(); os.Unsetenv("SAAS_LANG") })

	SetDefaultLang(LangEN)
	if Default() != LangEN {
		t.Fatal("显式默认语言应生效")
	}
	Reset()
	if Default() != LangZH && Default() != LangEN {
		t.Fatal("Reset 后应回到 env/回退链")
	}

	t.Setenv("SAAS_LANG", "en-US")
	Reset()
	if Default() != LangEN {
		t.Fatal("SAAS_LANG 应命中 en-US")
	}

	t.Setenv("SAAS_LANG", "ja-JP")
	if Default() != LangZH {
		t.Fatal("未注册语言应回退 zh-CN")
	}
}

func TestFromAccept(t *testing.T) {
	t.Cleanup(func() { Reset() })

	cases := []struct {
		header string
		want   Lang
	}{
		{"zh-CN,zh;q=0.9,en;q=0.8", LangZH},
		{"en-US;q=0.9,zh-CN;q=0.5", LangEN},
		{"en", LangEN}, // 裸主语言唯一命中
		{"en-US,en;q=0.8", LangEN},
		{"ja-JP", defaultLangAt()}, // 未注册 → 回退链
	}
	for _, c := range cases {
		Reset()
		if got := FromAccept(c.header); got != c.want {
			t.Errorf("FromAccept(%q) = %q, want %q", c.header, got, c.want)
		}
	}

	// 显式默认语言参与回退链
	SetDefaultLang(LangEN)
	Reset()
	SetDefaultLang(LangEN)
	if got := FromAccept("ja-JP"); got != LangEN {
		t.Errorf("显式默认语言应参与回退，got %q", got)
	}
	Reset()
}

func TestGenericKey(t *testing.T) {
	if v := Get(LangZH, KeySmsSent); v != "已发送" {
		t.Fatalf("通用键文案错误: %q", v)
	}
	if v := Get(LangEN, KeySmsSent); v != "Sent" {
		t.Fatalf("通用键文案错误: %q", v)
	}
}

// defaultLangAt 显式未设置且环境无 SAAS_LANG 时的回退语言。
func defaultLangAt() Lang {
	return LangZH
}
