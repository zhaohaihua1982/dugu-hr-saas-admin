// main —— dugu-hr-saas-admin 启动入口。
// 结构参考 go-demo/app/cmd/main.go：run() 包裹全部退出路径 + 信号驱动的优雅关停；
// 路由/业务层沿用本仓库的 gin-mall 标准分层（routes.NewRouter）。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/repository/cache"
	"dugu-hr-saas-admin/repository/db/dao"
	"dugu-hr-saas-admin/routes"
)

func main() {
	// log.Fatalf 会跳过 defer，所以资源释放必须放在 run 内部
	if err := run(); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

func run() error {
	// 收到 SIGINT/SIGTERM 时关闭 ctx，触发优雅退出，程序不再只能被 kill
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 加载配置 → MySQL（含迁移与超管 seed）→ Redis，任一步失败先释放在建资源
	if err := loading(); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:    addr(),
		Handler: newRouter(),
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
	// 等优雅关停结束，保证 defer closeApp() 在所有连接释放之后执行
	if err := <-shutdownDone; err != nil {
		log.Printf("优雅关停失败: %v", err)
	}
	return nil
}

// loading 启动依赖装配：配置 → MySQL → Redis。
// 任一步失败都会先释放已建立的资源，再交由调用方决定退出方式。
func loading() error {
	conf.InitConfig()

	// 生产环境禁止以内置默认密钥起跑（密钥必须 SAAS_* 注入）
	if conf.IsPro() {
		if strings.HasPrefix(conf.Config.EncryptSecret.JwtSecret, "dev-only") {
			return errors.New("生产环境必须通过 SAAS_JWT_SECRET/SAAS_MOBILE_HMAC/SAAS_MOBILE_AES 覆盖默认密钥")
		}
	}

	if err := dao.InitMySQL(); err != nil {
		return err
	}
	if err := cache.InitCache(); err != nil {
		_ = dao.Close() // 回滚已建立的 MySQL 连接
		return err
	}
	return nil
}

// closeApp 逆序释放依赖（Redis → MySQL）；多个错误合并返回。
func closeApp() {
	var errs []error
	if err := cache.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := dao.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		log.Printf("释放资源失败: %v", err)
	}
}

// newRouter 装配 gin 路由（http.Server 直接托管 gin.Engine）。
func newRouter() *gin.Engine {
	if !debugMode() {
		gin.SetMode(gin.ReleaseMode)
	}
	return routes.NewRouter()
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

// debugMode 从 SAAS_DEBUG 读取，开启后 GORM 打印 SQL 日志。
// 只认 true/false，其他值启动即失败，避免手滑写成 "ture" 时静默按 false 跑。
func debugMode() bool {
	v := strings.TrimSpace(os.Getenv("SAAS_DEBUG"))
	if v == "" {
		return conf.DebugMode() // 回落 yaml 的 system.appEnv 判定（dev/debug/local）
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		log.Fatalf("环境变量 SAAS_DEBUG=%q 不是合法布尔值（true/false）", v)
	}
	return b
}
