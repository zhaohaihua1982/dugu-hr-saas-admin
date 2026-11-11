// loginconfig 聚合校验测试（docs 登录配置设计 §6 规则逐条）。
package loginconfig

import (
	"encoding/json"
	"errors"
	"testing"
)

// validCfg 管理后台默认值必过校验（§3.2 seed 与校验规则自洽）。
func TestDefaultsValidate(t *testing.T) {
	for _, c := range []*Config{Defaults(TypeAdmin), Defaults(TypeTenant)} {
		if err := c.Validate(); err != nil {
			t.Fatalf("默认值应通过校验: %v", err)
		}
	}
}

func TestValidateRule1MaskZero(t *testing.T) {
	c := Defaults(TypeTenant)
	c.LoginMask = 0
	err := c.Validate()
	if !errors.Is(err, ErrBadParams) {
		t.Fatalf("mask=0 应报 ErrBadParams, got %v", err)
	}
	mustField(t, err, "login_mask")
}

func fieldOf(err error) string {
	var bf *BadField
	if errors.As(err, &bf) {
		return bf.Field
	}
	return ""
}

func mustField(t *testing.T, err error, want string) {
	t.Helper()
	if fieldOf(err) != want {
		t.Fatalf("违规字段应为 %q, got %q (%v)", want, fieldOf(err), err)
	}
}

func TestValidateRule2AdminKeepPwd(t *testing.T) {
	c := Defaults(TypeAdmin)
	c.LoginMask &^= BitUsernamePassword // 关 bit0
	if err := c.Validate(); !errors.Is(err, ErrMustKeepPwd) {
		t.Fatalf("admin 关密码登录应报 ErrMustKeepPwd, got %v", err)
	}
	// 租户侧不受此约束
	t2 := Defaults(TypeTenant)
	t2.LoginMask &^= BitUsernamePassword
	if err := t2.Validate(); err != nil {
		t.Fatalf("租户可关密码登录: %v", err)
	}
}

func TestValidateRule3LockVsThreshold(t *testing.T) {
	c := Defaults(TypeAdmin)
	c.FailThreshold, c.LockMinutes = 10, 15 // lock < threshold*2
	err := c.Validate()
	if !errors.Is(err, ErrBadParams) {
		t.Fatalf("应报 ErrBadParams, got %v", err)
	}
	mustField(t, err, "lock_minutes")
}

func TestValidateRule4TTLOrder(t *testing.T) {
	c := Defaults(TypeAdmin)
	c.RefreshTokenTTL = c.AccessTokenTTL // 相等也算错（严格的 < ）
	err := c.Validate()
	mustField(t, err, "refresh_token_ttl")

	c2 := Defaults(TypeAdmin)
	c2.SessionHardTTL = c2.RefreshTokenTTL - 1
	err = c2.Validate()
	mustField(t, err, "refresh_token_ttl")
}

func TestValidateRule5Remember(t *testing.T) {
	c := Defaults(TypeAdmin)
	c.RememberDays = c.RefreshTokenTTL/86400 + 1 // 超过 refresh 天数
	err := c.Validate()
	mustField(t, err, "remember_days")
}

func TestValidateRule6Ranges(t *testing.T) {
	bad := []struct {
		field  string
		mutate func(*Config)
	}{
		{"fail_threshold", func(c *Config) { c.FailThreshold = 2 }},
		{"captcha_after_fails", func(c *Config) { c.CaptchaAfterFails = 1 }},          // 0=关 / 2~5，1 非法
		{"pwd_min_classes", func(c *Config) { c.PwdMinClasses = c.PwdMinLength + 1 }}, // 类别数 > 长度
		{"session_max_concurrent", func(c *Config) { c.SessionMaxConcurrent = 11 }},
		{"ip_fail_limit", func(c *Config) { c.IPFailLimit = 9 }},
	}
	for _, b := range bad {
		c := Defaults(TypeAdmin)
		b.mutate(c)
		err := c.Validate()
		if !errors.Is(err, ErrBadParams) {
			t.Fatalf("%s 越界应报 ErrBadParams, got %v", b.field, err)
		}
		mustField(t, err, b.field)
	}
}

func TestValidateRule8Extras(t *testing.T) {
	c := Defaults(TypeAdmin)
	c.Extras = json.RawMessage(`{"mfa_mode":5}`)
	if err := c.Validate(); !errors.Is(err, ErrBadExtras) {
		t.Fatalf("mfa_mode 越界应报 ErrBadExtras, got %v", err)
	}
	c2 := Defaults(TypeAdmin)
	c2.Extras = json.RawMessage(`{"ip_whitelist":["999.1.1.1"]}`)
	if err := c2.Validate(); !errors.Is(err, ErrBadExtras) {
		t.Fatalf("非法 CIDR 应报 ErrBadExtras, got %v", err)
	}
	c3 := Defaults(TypeAdmin)
	c3.Extras = json.RawMessage(`{"ip_whitelist":["10.0.0.0/8","192.168.1.1"],"unknown_key":{"x":1}}`)
	if err := c3.Validate(); err != nil { // 未知 key 保留不校验不删（§8）
		t.Fatalf("合法 CIDR+未知 key 应通过: %v", err)
	}
}

func TestNormalizePwdLevel(t *testing.T) {
	cases := []struct {
		len, classes int
		forbid       bool
		want         int8
	}{
		{12, 3, true, 3}, {12, 3, false, 2}, {10, 2, true, 2}, {8, 1, false, 1}, {12, 4, true, 3},
	}
	for _, tc := range cases {
		c := Defaults(TypeAdmin)
		c.PwdMinLength, c.PwdMinClasses, c.PwdForbidUsername = tc.len, tc.classes, tc.forbid
		c.NormalizePwdLevel()
		if c.PwdLevel != tc.want {
			t.Fatalf("len=%d classes=%d forbid=%v → level %d, want %d", tc.len, tc.classes, tc.forbid, c.PwdLevel, tc.want)
		}
	}
}

func TestMergeExtras(t *testing.T) {
	base := json.RawMessage(`{"mfa_mode":1,"ip_whitelist":["10.0.0.0/8"],"future_key":true}`)
	merged, err := MergeExtras(base, json.RawMessage(`{"mfa_mode":2}`))
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(merged, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["mfa_mode"].(float64) != 2 {
		t.Fatalf("已知 key 应被提交覆盖: %v", m["mfa_mode"])
	}
	if _, ok := m["future_key"]; !ok {
		t.Fatalf("未知 key 应保留（向下兼容 §8）")
	}
	if _, ok := m["ip_whitelist"]; !ok {
		t.Fatalf("未提交 key 应保留")
	}
}
