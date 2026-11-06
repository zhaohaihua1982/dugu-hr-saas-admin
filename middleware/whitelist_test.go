// 白名单匹配测试。
package middleware

import "testing"

func TestWhitelistMatch(t *testing.T) {
	w := NewWhitelist("/healthz", "/api/v1/admin/auth/*")

	cases := []struct {
		path string
		want bool
	}{
		{"/healthz", true},
		{"/healthz/", false},                 // 精确匹配不吞斜杠变体
		{"/api/v1/admin/auth/captcha", true}, // 通配
		{"/api/v1/admin/auth/login/password", true},
		{"/api/v1/admin/auth", false},       // 前缀须完整到边界之内
		{"/api/v1/admin/auth/logout", true}, // 注意：logout 若需登录则不应列太宽
		{"/api/v1/admin/sys-users", false},
		{"/api/v1/admin/login-logs", false},
		{"/healthzz", false},
		{"", false},
	}
	for _, c := range cases {
		if got := w.Match(c.path); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestWhitelistNilAndEmpty(t *testing.T) {
	var nilWL *Whitelist
	if nilWL.Match("/anything") {
		t.Fatal("nil 白名单不应放行任何路径")
	}
	empty := NewWhitelist(" ")
	if empty.Match("/healthz") {
		t.Fatal("空参数白名单不应放行")
	}
}
