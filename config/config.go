// Package conf 配置加载：config/locales/config.yaml + 环境变量覆盖。
// 敏感项（密码、密钥）允许 SAAS_* 环境变量注入，避免硬编码进二进制。
package conf

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

var Config *Conf

type Conf struct {
	System         *System           `yaml:"system"`
	MySql          map[string]*MySql `yaml:"mysql"`
	Redis          *Redis            `yaml:"redis"`
	EncryptSecret  *EncryptSecret    `yaml:"encryptSecret"`
	BootstrapAdmin *BootstrapAdmin   `yaml:"bootstrapAdmin"`
	I18n           *I18n             `yaml:"i18n"`
}

// I18n 多语言配置：defaultLang 必须命中 pkg/i18n 已注册语言，
// 未设置或未注册时回退 zh-CN（也可由 SAAS_LANG 环境变量覆盖）。
type I18n struct {
	DefaultLang string `yaml:"defaultLang"`
}

type System struct {
	AppEnv   string `yaml:"appEnv"`
	HttpPort string `yaml:"httpPort"`
}

type MySql struct {
	Dialect  string `yaml:"dialect"`
	DbHost   string `yaml:"dbHost"`
	DbPort   string `yaml:"dbPort"`
	DbName   string `yaml:"dbName"`
	UserName string `yaml:"userName"`
	Password string `yaml:"password"`
	Charset  string `yaml:"charset"`
}

type Redis struct {
	RedisHost     string `yaml:"redisHost"`
	RedisPort     string `yaml:"redisPort"`
	RedisUsername string `yaml:"redisUsername"`
	RedisPassword string `yaml:"redisPwd"`
	RedisDbName   int    `yaml:"redisDbName"`
	RedisNetwork  string `yaml:"redisNetwork"`
}

// EncryptSecret 各类密钥：
//   - JwtSecret   会话 JWT 签名密钥
//   - MobileHmac  手机号 HMAC-SHA256 密钥（生成 mobile_hash，唯一索引查询入口）
//   - MobileAes   手机号 AES-256-GCM 密钥（64 位 hex → 32 字节，生成 mobile_enc）
type EncryptSecret struct {
	JwtSecret  string `yaml:"jwtSecret"`
	MobileHmac string `yaml:"mobileHmac"`
	MobileAes  string `yaml:"mobileAes"`
}

// BootstrapAdmin 初始超管账号（首次启动 seed 用，docs §8.3/P0 验收标准）。
type BootstrapAdmin struct {
	Password string `yaml:"password"` // 修改 SAAS_BOOTSTRAP_ADMIN_PASSWORD 注入
	Mobile   string `yaml:"mobile"`
	Email    string `yaml:"email"`
	Name     string `yaml:"name"`
}

// InitConfig 读 yaml → 覆盖环境变量 → 校验。
func InitConfig() {
	c, err := parseConfig()
	if err != nil {
		panic(err)
	}
	Config = c
}

func parseConfig() (*Conf, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("./config/locales")
	v.AddConfigPath("./config")
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	var c Conf
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	applyEnvOverrides(&c)

	if err := validate(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

// applyEnvOverrides 环境变量仅在存在时覆盖 yaml 值。
func applyEnvOverrides(c *Conf) {
	for name, my := range c.MySql {
		if my == nil {
			continue
		}
		prefix := "SAAS_MYSQL_" + strings.ToUpper(name) + "_"
		override(&my.UserName, prefix+"USER")
		override(&my.Password, prefix+"PASSWORD")
		override(&my.DbHost, prefix+"HOST")
		override(&my.DbPort, prefix+"PORT")
		override(&my.DbName, prefix+"NAME")
	}

	if c.System != nil {
		override(&c.System.HttpPort, "SAAS_HTTP_PORT")
	}

	if r := c.Redis; r != nil {
		override(&r.RedisHost, "SAAS_REDIS_HOST")
		override(&r.RedisPort, "SAAS_REDIS_PORT")
		override(&r.RedisUsername, "SAAS_REDIS_USERNAME")
		override(&r.RedisPassword, "SAAS_REDIS_PASSWORD")
		overrideInt(&r.RedisDbName, "SAAS_REDIS_DB")
	}

	if s := c.EncryptSecret; s != nil {
		override(&s.JwtSecret, "SAAS_JWT_SECRET")
		override(&s.MobileHmac, "SAAS_MOBILE_HMAC")
		override(&s.MobileAes, "SAAS_MOBILE_AES")
	}

	if b := c.BootstrapAdmin; b != nil {
		override(&b.Password, "SAAS_BOOTSTRAP_ADMIN_PASSWORD")
		override(&b.Mobile, "SAAS_BOOTSTRAP_ADMIN_MOBILE")
		override(&b.Email, "SAAS_BOOTSTRAP_ADMIN_EMAIL")
		override(&b.Name, "SAAS_BOOTSTRAP_ADMIN_NAME")
	}
}

func override(dst *string, key string) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		*dst = v
	}
}

func overrideInt(dst *int, key string) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

func validate(c *Conf) error {
	var errs []error

	my := c.MySql["default"]
	if my == nil {
		errs = append(errs, errors.New("mysql.default 未配置"))
	} else {
		if my.DbHost == "" || my.DbPort == "" || my.DbName == "" {
			errs = append(errs, errors.New("mysql.default host/port/dbName 不能为空"))
		}
	}

	if c.Redis == nil || c.Redis.RedisHost == "" || c.Redis.RedisPort == "" {
		errs = append(errs, errors.New("redis 未配置或不完整"))
	}

	if s := c.EncryptSecret; s == nil || len(s.JwtSecret) < 16 || len(s.MobileHmac) < 16 || len(s.MobileAes) != 64 {
		errs = append(errs, errors.New("encryptSecret 校验失败: jwtSecret/mobileHmac ≥16 位，mobileAes 须为 64 位 hex（AES-256 键）"))
	}

	if b := c.BootstrapAdmin; b == nil || b.Password == "" {
		errs = append(errs, errors.New("bootstrapAdmin.password 不能为空（SAAS_BOOTSTRAP_ADMIN_PASSWORD）"))
	}

	if p := c.System; p == nil || p.HttpPort == "" {
		errs = append(errs, errors.New("system.httpPort 不能为空"))
	}

	return errors.Join(errs...)
}

// DebugMode 返回当前是否调试环境（appEnv 属 dev/debug/local 即为真）。
func DebugMode() bool {
	a := Config.System.AppEnv
	return a == "dev" || a == "debug" || a == "local"
}

// IsPro 是否生产环境（生产环境禁止内置默认密钥启动）。
func IsPro() bool {
	a := Config.System.AppEnv
	return a == "prod" || a == "production"
}
