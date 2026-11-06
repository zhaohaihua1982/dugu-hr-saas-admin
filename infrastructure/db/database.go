// Package db —— MySQL 基础设施：连接池与迁移。
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	conf "dugu-hr-saas-admin/config"
)

// DB MySQL 会话持有者（gin 惯用的全局连接封装，全部语句必须以 .WithContext 克隆执行）。
type DB struct {
	gdb *gorm.DB
}

// Open 建立连接池并探活（不执行迁移）。
func Open(debug bool) (*DB, error) {
	mConfig := conf.Config.MySql["default"]

	logLevel := logger.Warn
	if debug {
		logLevel = logger.Info
	}

	dsn := mysqldriver.NewConfig()
	dsn.User = mConfig.UserName
	dsn.Passwd = mConfig.Password
	dsn.Net = "tcp"
	dsn.Addr = mConfig.DbHost + ":" + mConfig.DbPort
	dsn.DBName = mConfig.DbName
	dsn.ParseTime = true
	dsn.Timeout = 3 * time.Second
	dsn.ReadTimeout = 10 * time.Second
	dsn.WriteTimeout = 10 * time.Second
	dsn.Params = map[string]string{"charset": mConfig.Charset}

	gdb, err := gorm.Open(mysql.New(mysql.Config{
		DSNConfig:                 dsn,
		DefaultStringSize:         256,
		DisableDatetimePrecision:  true,
		DontSupportRenameIndex:    true,
		DontSupportRenameColumn:   true,
		SkipInitializeWithVersion: false,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		NamingStrategy: schema.NamingStrategy{
			SingularTable: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("连接 mysql 失败: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("获取 mysql 连接池失败: %w", err)
	}
	sqlDB.SetMaxIdleConns(20)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(30 * time.Minute) // 需小于 MySQL wait_timeout
	sqlDB.SetConnMaxIdleTime(10 * time.Minute)

	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("mysql 探活失败: %w", err)
	}

	return &DB{gdb: gdb.Set("gorm:table_options", "charset=utf8mb4")}, nil
}

// Close 释放连接池。
func (d *DB) Close() error {
	if d == nil || d.gdb == nil {
		return nil
	}
	sqlDB, err := d.gdb.DB()
	if err != nil {
		return fmt.Errorf("获取 mysql 连接池失败: %w", err)
	}
	return sqlDB.Close()
}

// WithContext ctx 绑定的会话（超时/取消随请求传播）。
// 使用纪律：finisher 一律在克隆上执行（本方法返回值），根连接 Statement 是共享可变状态。
func (d *DB) WithContext(ctx context.Context) *gorm.DB { return d.gdb.WithContext(ctx) }

// isDuplicate 唯一键冲突（1062）并归类索引。
func isDuplicate(err error) (index string, dup bool) {
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		return "", false
	}
	switch {
	case contains(mysqlErr.Message, "uk_username"):
		return "uk_username", true
	case contains(mysqlErr.Message, "uk_mobile"):
		return "uk_mobile", true
	}
	return "", true
}

func contains(s, sub string) bool { return len(sub) != 0 && stringIndex(s, sub) >= 0 }

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
