// Package loginconfig —— 登录配置聚合（docs/系统管理-登录配置设计.md）。
//
// 定位（§1）：登录管线是"执行代码"，本包是"策略数据"——只管策略怎么存、
// 怎么校验、怎么生效。标准DDD：零框架依赖（不 import gin/gorm/redis），
// 存储端口定义在此，由 infrastructure 实现（依赖倒置）。
package loginconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
)

// ---------- 配置类型 ----------

// ConfigType 配置类型（§1：每类型一行全局配置，不做按租户覆盖）。
type ConfigType int8

// TypeAdmin 管理后台登录（sys_user，安全策略取最严）；TypeTenant 租户登录。
const (
	TypeAdmin  ConfigType = 1
	TypeTenant ConfigType = 2
)

// Valid 类型合法值。
func (t ConfigType) Valid() bool { return t == TypeAdmin || t == TypeTenant }

// ---------- 登录组件位掩码（§2.1，与 i18n lang_mask 同一纪律：只增位不改义） ----------

const (
	BitUsernamePassword int64 = 1 << 0 // bit0 账号+密码（管理后台保底，admin 必留）
	BitMobilePassword   int64 = 1 << 1 // bit1 手机+密码（与 bit0 共用密码因子）
	BitMobileSms        int64 = 1 << 2 // bit2 手机+短信
	BitEmailPassword    int64 = 1 << 3 // bit3 邮箱+密码
	// bit4~bit7（16/32/64/128）预留：SSO / TOTP 独立因子 / 扫码 / 微信。
)

// Has 判断组件是否启用。
func Has(mask int64, bit int64) bool { return mask&bit != 0 }

// ---------- 解锁方式 / 到期动作 / 密码等级 ----------

// UnlockMode 解锁方式：1 自动到期 2 仅管理员 3 两者皆可。
const (
	UnlockAuto  int8 = 1
	UnlockAdmin int8 = 2
	UnlockBoth  int8 = 3
)

// PwdExpireAction 密码到期动作：1 提示不强制 2 强制改密。
const (
	PwdExpireHint   int8 = 1
	PwdExpireForced int8 = 2
)

// ---------- 聚合根 ----------

// Config 登录配置聚合（§3.1 单表每类型一行；无 deleted_at/status——配置行不可删，
// 停用语义由字段值表达）。
type Config struct {
	ID         int64      `json:"id"`
	ConfigType ConfigType `json:"config_type"`
	ScopeID    int64      `json:"scope_id"` // 0=全局；预留按租户覆盖

	// 登录组件开关
	LoginMask int64 `json:"login_mask"`

	// 账号保护
	FailThreshold     int  `json:"fail_threshold"`      // 3~20
	LockMinutes       int  `json:"lock_minutes"`        // 1~1440
	UnlockMode        int8 `json:"unlock_mode"`         // 1/2/3
	CaptchaAfterFails int  `json:"captcha_after_fails"` // 0=关闭 / 2~5
	IPFailLimit       int  `json:"ip_fail_limit"`       // 次/小时 10~1000
	IPRateLimit       int  `json:"ip_rate_limit"`       // 次/分钟 5~600

	// 会话与 Token（单位秒）
	AccessTokenTTL       int  `json:"access_token_ttl"`       // 300~86400
	RefreshTokenTTL      int  `json:"refresh_token_ttl"`      // 3600~2592000
	SessionHardTTL       int  `json:"session_hard_ttl"`       // ≥ access_token_ttl
	SessionMaxConcurrent int  `json:"session_max_concurrent"` // 0=不限 / 1~10
	KickOnPwdChange      bool `json:"kick_on_pwd_change"`     // 修改密码是否踢下线
	RememberDays         int  `json:"remember_days"`          // 0=关闭 / 0~30

	// 密码复杂度（§2.4：明细字段是运行时唯一真相，等级仅预设展示）
	PwdLevel            int8 `json:"pwd_level"`              // 1低 2中 3高（保存时按明细反推）
	PwdMinLength        int  `json:"pwd_min_length"`         // 8~32
	PwdMinClasses       int  `json:"pwd_min_classes"`        // 1~4 且 ≤ 长度
	PwdForbidUsername   bool `json:"pwd_forbid_username"`    // 禁止包含账号名
	PwdMaxAgeDays       int  `json:"pwd_max_age_days"`       // 0=不过期
	PwdExpireAction     int8 `json:"pwd_expire_action"`      // 1提示 2强制
	PwdRemindDays       int  `json:"pwd_remind_days"`        // 到期提前提醒
	PwdHistoryCount     int  `json:"pwd_history_count"`      // 0=不限制 / 1~24
	PwdForceFirstChange bool `json:"pwd_force_first_change"` // 重置密码后置位用户标记
	AllowSelfChangePwd  bool `json:"allow_self_change_pwd"`

	// 扩展项（§2.5：MFA/IP白名单/登录时段等预留项；未知 key 保留不删）
	Extras json.RawMessage `json:"extras"`

	Version      int    `json:"version"`    // 乐观锁
	UpdatedBy    int64  `json:"updated_by"` // 最后修改人（审计）
	CreatedAtStr string `json:"-"`          // 仓储填充（展示用，域内不依赖格式化）
	UpdatedAtStr string `json:"-"`
}

// ---------- 代码兜底默认值（§3.2/§3.3：与 seed 保持同一份常量） ----------

// DefaultAdminID / DefaultTenantID 固定 seed ID（§3.2；仅建行时使用）。
const (
	DefaultAdminID  int64 = 920000000000000001
	DefaultTenantID int64 = 920000000000000002
)

// Defaults 返回指定类型的默认配置（§3.2 seed 值——provider 兜底、reset 共用；
// 兜底语义是"最严默认"，不是"无配置"）。
func Defaults(t ConfigType) *Config {
	switch t {
	case TypeAdmin:
		// 管理后台：最严。不开手机短信（mask=3 非 7，防枚举 §3.2）；并发会话=1。
		// v1.1：RefreshTokenTTL 604800→86400（规则 4 自洽修正，用户裁定）。
		return &Config{ConfigType: TypeAdmin, ScopeID: 0, LoginMask: 3,
			FailThreshold: 5, LockMinutes: 15, UnlockMode: UnlockBoth,
			CaptchaAfterFails: 3, IPFailLimit: 50, IPRateLimit: 20,
			AccessTokenTTL: 7200, RefreshTokenTTL: 86400, SessionHardTTL: 86400,
			SessionMaxConcurrent: 1, KickOnPwdChange: true, RememberDays: 0,
			PwdLevel: 3, PwdMinLength: 12, PwdMinClasses: 3, PwdForbidUsername: true,
			PwdMaxAgeDays: 180, PwdExpireAction: PwdExpireHint, PwdRemindDays: 7,
			PwdHistoryCount: 3, PwdForceFirstChange: false, AllowSelfChangePwd: false}
	case TypeTenant:
		// 租户：组件全开（mask=15），策略中档。v1.1：RefreshTokenTTL 同步修正。
		return &Config{ConfigType: TypeTenant, ScopeID: 0, LoginMask: 15,
			FailThreshold: 5, LockMinutes: 15, UnlockMode: UnlockBoth,
			CaptchaAfterFails: 3, IPFailLimit: 50, IPRateLimit: 20,
			AccessTokenTTL: 7200, RefreshTokenTTL: 86400, SessionHardTTL: 86400,
			SessionMaxConcurrent: 3, KickOnPwdChange: true, RememberDays: 0,
			PwdLevel: 2, PwdMinLength: 10, PwdMinClasses: 2, PwdForbidUsername: true,
			PwdMaxAgeDays: 0, PwdExpireAction: PwdExpireHint, PwdRemindDays: 7,
			PwdHistoryCount: 3, PwdForceFirstChange: false, AllowSelfChangePwd: true}
	}
	return nil
}

// SeedID 固定 seed 行 ID（建行用；类型非法返回 0）。
func (t ConfigType) SeedID() int64 {
	switch t {
	case TypeAdmin:
		return DefaultAdminID
	case TypeTenant:
		return DefaultTenantID
	}
	return 0
}

// ---------- 领域错误 ----------

var (
	// ErrNotFound 配置行不存在（seed 漏跑/表损坏）。
	ErrNotFound = errors.New("loginconfig: not found")
	// ErrBadParams 参数不合法（BadField 包装携带违规字段名，errors.Is 语义不变）。
	ErrBadParams = errors.New("loginconfig: bad params")
	// ErrVersionConflict 乐观锁冲突（两个管理员并发保存，§8）。
	ErrVersionConflict = errors.New("loginconfig: version conflict")
	// ErrMustKeepPwd 管理后台必须保留"账号+密码"登录（§2.1 纪律 3，硬约束单列）。
	ErrMustKeepPwd = errors.New("loginconfig: admin must keep username+password login")
	// ErrBadExtras extras 扩展项格式错误（§6 规则 8）。
	ErrBadExtras = errors.New("loginconfig: bad extras")
)

// BadField 参数校验失败的字段提示（与字典 v1.5 同款：包装 ErrBadParams，
// api 层读 Field 放进响应 data）。
type BadField struct {
	Field string
	Err   error
}

func (e *BadField) Error() string { return e.Err.Error() + " [" + e.Field + "]" }
func (e *BadField) Unwrap() error { return e.Err }

func badParam(field string) error { return &BadField{Field: field, Err: ErrBadParams} }

// ---------- 校验（§6，服务端铁律） ----------

// Validate 保存前全量校验（§6 规则 1/2/3/4/5/6/8；规则 7 等级反推见 normalizePwdLevel）。
func (c *Config) Validate() error {
	// 各字段取值范围（§2.2/§2.3/§2.4 表）
	ranges := []struct {
		field string
		ok    bool
	}{
		{"fail_threshold", c.FailThreshold >= 3 && c.FailThreshold <= 20},
		{"lock_minutes", c.LockMinutes >= 1 && c.LockMinutes <= 1440},
		{"unlock_mode", c.UnlockMode >= UnlockAuto && c.UnlockMode <= UnlockBoth},
		{"captcha_after_fails", c.CaptchaAfterFails == 0 || (c.CaptchaAfterFails >= 2 && c.CaptchaAfterFails <= 5)},
		{"ip_fail_limit", c.IPFailLimit >= 10 && c.IPFailLimit <= 1000},
		{"ip_rate_limit", c.IPRateLimit >= 5 && c.IPRateLimit <= 600},
		{"access_token_ttl", c.AccessTokenTTL >= 300 && c.AccessTokenTTL <= 86400},
		{"refresh_token_ttl", c.RefreshTokenTTL >= 3600 && c.RefreshTokenTTL <= 2592000},
		{"session_max_concurrent", c.SessionMaxConcurrent == 0 || (c.SessionMaxConcurrent >= 1 && c.SessionMaxConcurrent <= 10)},
		{"remember_days", c.RememberDays >= 0 && c.RememberDays <= 30},
		{"pwd_min_length", c.PwdMinLength >= 8 && c.PwdMinLength <= 32},
		{"pwd_min_classes", c.PwdMinClasses >= 1 && c.PwdMinClasses <= 4 && c.PwdMinClasses <= c.PwdMinLength},
		{"pwd_max_age_days", c.PwdMaxAgeDays >= 0},
		{"pwd_expire_action", c.PwdExpireAction == PwdExpireHint || c.PwdExpireAction == PwdExpireForced},
		{"pwd_remind_days", c.PwdRemindDays >= 0 && c.PwdRemindDays <= 365},
		{"pwd_history_count", c.PwdHistoryCount >= 0 && c.PwdHistoryCount <= 24},
	}
	for _, r := range ranges {
		if !r.ok {
			return badParam(r.field)
		}
	}

	// 规则 1：至少启用一种登录方式
	if c.LoginMask == 0 {
		return badParam("login_mask")
	}
	// 规则 2：admin 配置必须保留 bit0（超管"必须密码登录"，§2.1 纪律 3）
	if c.ConfigType == TypeAdmin && !Has(c.LoginMask, BitUsernamePassword) {
		return ErrMustKeepPwd
	}
	// 规则 3：冻结时长与错误次数匹配（防止"又松又短"）
	if c.LockMinutes < c.FailThreshold*2 {
		return badParam("lock_minutes")
	}
	// 规则 4：Token 有效期层级（短期的不能比长期长；refresh 不能超过硬上限）
	if c.AccessTokenTTL >= c.RefreshTokenTTL || c.RefreshTokenTTL > c.SessionHardTTL {
		return badParam("refresh_token_ttl")
	}
	// 规则 5：记住登录不能超过 refreshToken 有效期
	if c.RememberDays > c.RefreshTokenTTL/86400 {
		return badParam("remember_days")
	}
	// 规则 8：extras schema（未知 key 保留不删，§8）
	if err := validateExtras(c.Extras); err != nil {
		return err
	}
	return nil
}

// NormalizePwdLevel 规则 7：以明细为准反推等级回写（明细是唯一真相，§2.4）。
// 确定性规则（登记于设计文档 §11）：高=长度≥12 且 类别≥3 且禁账号名；
// 中=长度≥10 且 类别≥2；否则低。
func (c *Config) NormalizePwdLevel() {
	switch {
	case c.PwdMinLength >= 12 && c.PwdMinClasses >= 3 && c.PwdForbidUsername:
		c.PwdLevel = 3
	case c.PwdMinLength >= 10 && c.PwdMinClasses >= 2:
		c.PwdLevel = 2
	default:
		c.PwdLevel = 1
	}
}

// extrasKeys extras 已登记的 key（§2.5；未登记 key 保留不删——向下兼容）。
var extrasKeys = map[string]bool{
	"mfa_mode": true, "mfa_grace_days": true, "ip_whitelist": true, "login_time_range": true,
}

// validateExtras §6 规则 8：mfa_mode 枚举、ip_whitelist 合法 CIDR/IP 数组；
// 未知 key 不校验不删除。
func validateExtras(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("%w: 不是 JSON 对象", ErrBadExtras)
	}
	if v, ok := m["mfa_mode"]; ok {
		var n int
		if err := json.Unmarshal(v, &n); err != nil || n < 0 || n > 2 {
			return fmt.Errorf("%w: mfa_mode 枚举非法", ErrBadExtras)
		}
	}
	if v, ok := m["mfa_grace_days"]; ok {
		var n int
		if err := json.Unmarshal(v, &n); err != nil || n < 0 {
			return fmt.Errorf("%w: mfa_grace_days 非法", ErrBadExtras)
		}
	}
	if v, ok := m["ip_whitelist"]; ok {
		var list []string
		if err := json.Unmarshal(v, &list); err != nil {
			return fmt.Errorf("%w: ip_whitelist 须为字符串数组", ErrBadExtras)
		}
		for _, s := range list {
			if !validCIDROrIP(s) {
				return fmt.Errorf("%w: ip_whitelist 含非法 CIDR/IP %q", ErrBadExtras, s)
			}
		}
	}
	return nil
}

// validCIDROrIP 接受 CIDR（10.0.0.0/8）或裸 IP（按主机地址处理）。
func validCIDROrIP(s string) bool {
	s = strings.TrimSpace(s)
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	return net.ParseIP(s) != nil
}

// MergeExtras 合并扩展项：已知 key 以本次提交为准，未知 key 保留（§8 边界）。
// base=库里现值，patch=本次提交（均可为空）。
func MergeExtras(base, patch json.RawMessage) (json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	for _, raw := range []json.RawMessage{base, patch} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("%w: extras 不是 JSON 对象", ErrBadExtras)
		}
		for k, v := range m {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	// key 排序保证输出稳定（encoding/json 对 map 按键序序列化，无序问题）
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// ExtrasMFAMode 读 mfa_mode（预留项类型安全读取；缺省 0）。
func ExtrasMFAMode(raw json.RawMessage) int {
	var m map[string]int
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0
	}
	return m["mfa_mode"]
}
