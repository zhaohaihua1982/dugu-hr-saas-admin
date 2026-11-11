// Package po —— 表对象（PO）：字段与 config/sql/*.sql 中的 DDL 一一对应，仅本包使用。
package po

import "time"

// SysUser sys_user 表对象（docs §3.1）。软删参与唯一键：deleted_at 默认 0，删除时写毫秒戳。
type SysUser struct {
	ID             int64      `gorm:"primaryKey;column:id"` // 雪花ID，应用侧生成
	Username       string     `gorm:"column:username"`
	PasswordHash   string     `gorm:"column:password_hash"`
	PasswordAlgo   string     `gorm:"column:password_algo"`
	Salt           string     `gorm:"column:salt"`
	Name           string     `gorm:"column:name"`
	MobileHash     string     `gorm:"column:mobile_hash"`
	MobileEnc      string     `gorm:"column:mobile_enc"`
	MobileMask     string     `gorm:"column:mobile_mask"`
	Email          string     `gorm:"column:email"`
	Status         int        `gorm:"column:status"`
	FailCount      int        `gorm:"column:fail_count"`
	LockedUntil    *time.Time `gorm:"column:locked_until"`
	LastLoginAt    *time.Time `gorm:"column:last_login_at"`
	LastLoginIP    string     `gorm:"column:last_login_ip"`
	PwdUpdatedAt   *time.Time `gorm:"column:pwd_updated_at"`
	PwdForceChange bool       `gorm:"column:pwd_force_change"` // 首登强制改密标记（登录配置 P1.5）
	DeletedAt      int64      `gorm:"column:deleted_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

// TableName 表名。
func (SysUser) TableName() string { return "sys_user" }

// SysLoginLog sys_login_log 表对象（docs §3.2；v1.2：identifier 账号原样/手机脱敏）。
type SysLoginLog struct {
	ID             int64     `gorm:"primaryKey;column:id"`
	UserID         int64     `gorm:"column:user_id"`
	IdentifierMask string    `gorm:"column:identifier_mask"`
	LoginType      string    `gorm:"column:login_type"`
	IdentVia       string    `gorm:"column:ident_via"`
	Success        bool      `gorm:"column:success"`
	FailReason     string    `gorm:"column:fail_reason"`
	IP             string    `gorm:"column:ip"`
	UA             string    `gorm:"column:ua"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

// TableName 表名。
func (SysLoginLog) TableName() string { return "sys_login_log" }

// SysMenu sys_menu 表对象（docs 三模块设计 §6；action_uniq 为生成列，不经本结构体写）。
type SysMenu struct {
	ID        int64     `gorm:"primaryKey;column:id"` // 雪花ID，应用侧生成
	ParentID  int64     `gorm:"column:parent_id"`
	Name      string    `gorm:"column:name"`
	Type      int       `gorm:"column:type"`
	Path      string    `gorm:"column:path"`
	Component string    `gorm:"column:component"`
	Icon      string    `gorm:"column:icon"`
	Sort      int       `gorm:"column:sort"`
	IDPath    string    `gorm:"column:id_path"` // 父链ID串：根=自身；子="父id/.../自身"
	Visible   bool      `gorm:"column:visible"`
	Status    int       `gorm:"column:status"`
	Code      string    `gorm:"column:code"`
	Remark    string    `gorm:"column:remark"`
	DeletedAt int64     `gorm:"column:deleted_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 表名。
func (SysMenu) TableName() string { return "sys_menu" }

// SysI18nText 统一翻译表 sys_i18n_text（数据文案唯一存储，docs 基础设施-i18n设计 §2）。
// v1.5 合并粒度：一行 = 资源 × 字段，全部语言进 texts JSON（取代"一语言一行"）。
type SysI18nText struct {
	ID        int64     `gorm:"primaryKey;column:id"`
	ResType   string    `gorm:"column:res_type"`
	ResID     int64     `gorm:"column:res_id"`
	Field     string    `gorm:"column:field"`
	Texts     string    `gorm:"column:texts;type:json"` // {lang: 译文}；不含默认语言（§7.4）
	LangMask  int64     `gorm:"column:lang_mask"`       // 位掩码：已录入语言（位分配 domain/i18n.LangBit，只许追加）
	Complete  int8      `gorm:"column:complete"`        // 1=非默认语言的已注册语言全部录入（写入时重算）
	DeletedAt int64     `gorm:"column:deleted_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 表名。
func (SysI18nText) TableName() string { return "sys_i18n_text" }

// SysLoginConfig sys_login_config 表对象（docs 登录配置设计 §3.1）。
// 配置行不可删、无软删/状态列；每类型一行（uk_config）。
type SysLoginConfig struct {
	ID                   int64     `gorm:"primaryKey;column:id"`
	ConfigType           int8      `gorm:"column:config_type"`
	ScopeID              int64     `gorm:"column:scope_id"`
	LoginMask            int64     `gorm:"column:login_mask"`
	FailThreshold        int16     `gorm:"column:fail_threshold"`
	LockMinutes          int16     `gorm:"column:lock_minutes"`
	UnlockMode           int8      `gorm:"column:unlock_mode"`
	CaptchaAfterFails    int16     `gorm:"column:captcha_after_fails"`
	IPFailLimit          int16     `gorm:"column:ip_fail_limit"`
	IPRateLimit          int16     `gorm:"column:ip_rate_limit"`
	AccessTokenTTL       int32     `gorm:"column:access_token_ttl"`
	RefreshTokenTTL      int32     `gorm:"column:refresh_token_ttl"`
	SessionHardTTL       int32     `gorm:"column:session_hard_ttl"`
	SessionMaxConcurrent int16     `gorm:"column:session_max_concurrent"`
	KickOnPwdChange      bool      `gorm:"column:kick_on_pwd_change"`
	RememberDays         int16     `gorm:"column:remember_days"`
	PwdLevel             int8      `gorm:"column:pwd_level"`
	PwdMinLength         int16     `gorm:"column:pwd_min_length"`
	PwdMinClasses        int16     `gorm:"column:pwd_min_classes"`
	PwdForbidUsername    bool      `gorm:"column:pwd_forbid_username"`
	PwdMaxAgeDays        int32     `gorm:"column:pwd_max_age_days"`
	PwdExpireAction      int8      `gorm:"column:pwd_expire_action"`
	PwdRemindDays        int16     `gorm:"column:pwd_remind_days"`
	PwdHistoryCount      int16     `gorm:"column:pwd_history_count"`
	PwdForceFirstChange  bool      `gorm:"column:pwd_force_first_change"`
	AllowSelfChangePwd   bool      `gorm:"column:allow_self_change_pwd"`
	Extras               string    `gorm:"column:extras;type:json"`
	Version              int       `gorm:"column:version"`
	UpdatedBy            int64     `gorm:"column:updated_by"`
	CreatedAt            time.Time `gorm:"column:created_at"`
	UpdatedAt            time.Time `gorm:"column:updated_at"`
}

// TableName 表名。
func (SysLoginConfig) TableName() string { return "sys_login_config" }

// SysPasswordHistory sys_password_history 表对象（docs 登录配置设计 §10 P1.5）。
// 只增不改；改密时写旧 hash，pwd_history_count 校验逐条 bcrypt 比对最近 N 行。
type SysPasswordHistory struct {
	ID           int64     `gorm:"primaryKey;column:id"`
	UserType     int8      `gorm:"column:user_type"`
	UserID       int64     `gorm:"column:user_id"`
	PasswordHash string    `gorm:"column:password_hash"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// TableName 表名。
func (SysPasswordHistory) TableName() string { return "sys_password_history" }

// SysDictType sys_dict_type 表对象（docs 字典设计 §1.2）。软删参与唯一键：deleted_at 默认 0。
type SysDictType struct {
	ID        int64     `gorm:"primaryKey;column:id"` // 雪花ID，应用侧生成
	Code      string    `gorm:"column:code"`
	Name      string    `gorm:"column:name"`
	Status    int8      `gorm:"column:status"`
	Remark    string    `gorm:"column:remark"`
	DeletedAt int64     `gorm:"column:deleted_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 表名。
func (SysDictType) TableName() string { return "sys_dict_type" }

// SysDictItem sys_dict_item 表对象（docs 字典设计 §1.2）。
// label 是默认语言显示名（兜底）；其他语言的译文在统一翻译表（res_type='dict_item'）。
type SysDictItem struct {
	ID        int64     `gorm:"primaryKey;column:id"` // 雪花ID，应用侧生成
	TypeID    int64     `gorm:"column:type_id"`
	Label     string    `gorm:"column:label"`
	Value     string    `gorm:"column:value"`
	Sort      int       `gorm:"column:sort"`
	Status    int8      `gorm:"column:status"`
	Remark    string    `gorm:"column:remark"`
	DeletedAt int64     `gorm:"column:deleted_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 表名。
func (SysDictItem) TableName() string { return "sys_dict_item" }
