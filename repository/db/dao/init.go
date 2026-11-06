// Package dao 仓储实现（gin-mall 标准：dao 持有 ctx，内部取全局 _db）。
package dao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	conf "dugu-hr-saas-admin/config"
)

var _db *gorm.DB

// InitMySQL 建立连接池、执行迁移并 seed 初始超管。
// 出错返回 error 由启动方决定退出方式（参考 go-demo：不再内部 panic）。
func InitMySQL() error {
	mConfig := conf.Config.MySql["default"]

	var ormLogger logger.Interface
	if conf.DebugMode() {
		ormLogger = logger.Default.LogMode(logger.Info)
	} else {
		ormLogger = logger.Default
	}

	dsn := mysqldriver.NewConfig()
	dsn.User = mConfig.UserName
	dsn.Passwd = mConfig.Password
	dsn.Net = "tcp"
	dsn.Addr = mConfig.DbHost + ":" + mConfig.DbPort
	dsn.DBName = mConfig.DbName
	dsn.ParseTime = true
	dsn.Timeout = 3 * time.Second
	dsn.Params = map[string]string{"charset": mConfig.Charset}

	db, err := gorm.Open(mysql.New(mysql.Config{
		DSNConfig:                 dsn,
		DefaultStringSize:         256,
		DisableDatetimePrecision:  true,
		DontSupportRenameIndex:    true,
		DontSupportRenameColumn:   true,
		SkipInitializeWithVersion: false,
	}), &gorm.Config{
		Logger: ormLogger,
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return fmt.Errorf("连接 mysql 失败: %w", err)
	}

	sqlDB, _ := db.DB()
	sqlDB.SetMaxIdleConns(20)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	_db = db.Set("gorm:table_options", "charset=utf8mb4")

	if err := migrate(); err != nil {
		return fmt.Errorf("数据库迁移失败: %w", err)
	}
	if err := seedSuperAdmin(); err != nil {
		return fmt.Errorf("初始超管账号 seed 失败: %w", err)
	}
	return nil
}

// Close 释放 MySQL 连接池（优雅退出时调用）。
func Close() error {
	if _db == nil {
		return nil
	}
	sqlDB, err := _db.DB()
	if err != nil {
		return fmt.Errorf("获取 mysql 连接池失败: %w", err)
	}
	return sqlDB.Close()
}

// NewDBClient 获得 ctx 绑定的会话连接（超时/取消随请求传播）。
//
// 使用纪律：任何 finisher（Create/First/Find/Update/Delete/Exec/…）都必须在
// 本方法返回的克隆上调用，禁止直接操作根连接 _db——根连接的 Statement 是
// 共享可变状态，污染一次会让后续克隆的 schema 与值类型错串（肉眼难查的
// “reflect: Field index out of range”）。
func NewDBClient(ctx context.Context) *gorm.DB {
	return _db.WithContext(ctx)
}

// isDuplicate 判断错误是否唯一键冲突（1062），并返回命中的索引名。
// "" 表示冲突但未能识别索引（例如历史数据其他唯一键）。
func isDuplicate(err error) (index string, dup bool) {
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		return "", false
	}
	switch {
	case strings.Contains(mysqlErr.Message, "uk_username"):
		return "uk_username", true
	case strings.Contains(mysqlErr.Message, "uk_mobile"):
		return "uk_mobile", true
	}
	return "", true
}
