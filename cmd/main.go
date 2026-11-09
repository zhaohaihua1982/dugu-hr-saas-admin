// main —— dugu-hr-saas-admin 启动入口。
// 结构参考 go-demo：run() 包裹全部退出路径 + 信号驱动优雅关停。
// 装配严格按标准DDD依赖方向：interfaces ← application ← domain ← infrastructure 实现。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/application"
	appauth "dugu-hr-saas-admin/application/auth"
	apploginlog "dugu-hr-saas-admin/application/loginlog"
	appmenus "dugu-hr-saas-admin/application/menus"
	appsysuser "dugu-hr-saas-admin/application/sysuser"
	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/sysuser"
	"dugu-hr-saas-admin/infrastructure/cache"
	"dugu-hr-saas-admin/infrastructure/captcha"
	"dugu-hr-saas-admin/infrastructure/crypto"
	dbpkg "dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/dao"
	idpkg "dugu-hr-saas-admin/infrastructure/id"
	"dugu-hr-saas-admin/infrastructure/token"
	"dugu-hr-saas-admin/pkg/i18n"
	"dugu-hr-saas-admin/routes"
)

// assembled 装配后的应用句柄（gin 引擎 + 依赖释放）。
type assembled struct {
	router      *gin.Engine
	releaseHost func() error
}

func main() {
	// log.Fatalf 会跳过 defer，所以资源释放必须放在 run 内部
	if err := run(); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

func run() error {
	// 收到 SIGINT/SIGTERM 时关闭 ctx，优雅退出，程序不再只能被 kill
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := assemble(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := a.releaseHost(); err != nil {
			log.Printf("释放资源失败: %v", err)
		}
	}()

	srv := &http.Server{
		Addr:    addr(),
		Handler: a.router,
		// ReadHeaderTimeout 防止恶意客户端只发一个请求头占住连接（slowloris）
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ctx 关闭时开始优雅关停：等存量请求处理完，最多等 5s
	shutdownDone := make(chan error, 1)
	go func() {
		<-ctx.Done()
		log.Printf("收到退出信号，开始优雅关停…")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownDone <- srv.Shutdown(shutdownCtx)
	}()

	log.Printf("HTTP 服务已启动: %s", srv.Addr)
	// Shutdown 成功后 ListenAndServe 返回 ErrServerClosed，这是正常路径不是错误
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if err := <-shutdownDone; err != nil {
		log.Printf("优雅关停失败: %v", err)
	}
	return nil
}

// assemble 标准DDD装配（依赖方向自下而上）：
//
//	conf → infrastructure(db/cache/crypto/token/id/captcha)
//	  → 以 domain 端口注入 application 用例
//	  → routes(interfaces) 只引用 application
func assemble(ctx context.Context) (*assembled, error) {
	conf.InitConfig()
	debug := debugMode()

	// 应用默认语言（yaml 显式配置优先于 SAAS_LANG 环境变量）
	if c := conf.Config.I18n; c != nil && strings.TrimSpace(c.DefaultLang) != "" {
		if l, ok := i18n.ParseLang(strings.TrimSpace(c.DefaultLang)); ok {
			i18n.SetDefaultLang(l)
		} else {
			log.Printf("i18n.defaultLang=%q 未注册，回退默认语言链", c.DefaultLang)
		}
	}

	// 生产环境禁止以内置默认密钥起跑（docs：密钥必须 SAAS_* 注入）
	if conf.IsPro() && strings.HasPrefix(conf.Config.EncryptSecret.JwtSecret, "dev-only") {
		return nil, errors.New("生产环境必须通过 SAAS_JWT_SECRET/SAAS_MOBILE_HMAC/SAAS_MOBILE_AES 覆盖默认密钥")
	}

	// ---------- infrastructure ----------
	database, err := dbpkg.Open(debug)
	if err != nil {
		return nil, err
	}
	if err := database.Migrate(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	redisClient, err := cache.Open()
	if err != nil {
		_ = database.Close()
		return nil, err
	}

	// ---------- infrastructure 实现（实现 domain 端口） ----------
	mobileCrypto, hErr := crypto.NewMobileCrypto(
		conf.Config.EncryptSecret.MobileHmac, conf.Config.EncryptSecret.MobileAes)
	if hErr != nil {
		_ = redisClient.Close()
		_ = database.Close()
		return nil, hErr
	}
	hasher := crypto.NewPasswordHasher()
	tokens := token.New(conf.Config.EncryptSecret.JwtSecret)
	ids, iErr := idpkg.NewSnowflake(1)
	if iErr != nil {
		_ = redisClient.Close()
		_ = database.Close()
		return nil, iErr
	}

	users := dao.NewSysUserRepository(database)
	logsRepo := dao.NewLoginLogRepository(database)
	if sErr := database.SeedSuperAdmin(ctx, users, *conf.Config.BootstrapAdmin, hasher, mobileCrypto, ids); sErr != nil {
		_ = redisClient.Close()
		_ = database.Close()
		return nil, sErr
	}

	sessions := cache.NewSessionStore(redisClient)
	captchaSvc := cache.NewCaptchaService(redisClient, captcha.Generate)
	guard := cache.NewLoginGuard(redisClient)
	lock := cache.NewLockGuard(redisClient)
	codes := cache.NewSmsCodeStore(redisClient)

	// ---------- application ----------
	svc := &application.Services{
		Auth: appauth.NewService(users, logsRepo, tokens, sessions, captchaSvc,
			guard, lock, codes, smsSenderStub{}, mobileCrypto, hasher, ids),
		Users: appsysuser.NewService(users, sessions, lock, mobileCrypto, hasher, ids),
		Logs:  apploginlog.NewService(logsRepo),
		Menus: appmenus.NewService(dao.NewMenuRepository(database, ids), ids),
	}

	// ---------- interfaces ----------
	router := routes.NewRouter(svc, tokens, sessions)

	return &assembled{router: router, releaseHost: func() error {
		var errs []error
		if err := redisClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 redis: %w", err))
		}
		if err := database.Close(); err != nil {
			errs = append(errs, fmt.Errorf("关闭 mysql: %w", err))
		}
		return errors.Join(errs...)
	}}, nil
}

// smsSenderStub 短信通道开发桩：实现 authn.SmsSender。接入真实通道（阿里云/腾讯云）
// 时替换实现即可，登录管线与配额规则不变（docs §5）。
type smsSenderStub struct{}

func (smsSenderStub) Send(_ context.Context, mobile, code string) error {
	slog.Info("sms.send_stub 验证码已生成（生产环境请替换真实通道）", "mobile", sysuser.MaskMobile(mobile), "code", code)
	return nil
}

// addr 依次取 SAAS_ADDR 环境变量、yaml system.httpPort，缺省 :8080。
func addr() string {
	if v := strings.TrimSpace(os.Getenv("SAAS_ADDR")); v != "" {
		return v
	}
	if c := conf.Config; c != nil && c.System != nil && strings.TrimSpace(c.System.HttpPort) != "" {
		return strings.TrimSpace(c.System.HttpPort)
	}
	return ":8080"
}

// debugMode 从 SAAS_DEBUG 读取；只认 true/false，其他值启动即失败。
func debugMode() bool {
	v := strings.TrimSpace(os.Getenv("SAAS_DEBUG"))
	if v == "" {
		return conf.DebugMode()
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Fatalf("环境变量 SAAS_DEBUG=%q 不是合法布尔值（true/false）", v)
	}
	return b
}

// 编译期断言：桩实现满足端口。
var _ authn.SmsSender = smsSenderStub{}
