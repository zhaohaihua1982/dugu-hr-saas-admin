// Package cache Redis 访问层：会话、验证码、短信码、失败计数与限流。
// docs §6 的铁律：Redis 是加速，DB 是兜底——所有安全判定都不允许
// “Redis 挂了就放行”，必须回到 DB 判定（见 fail.go 的 Fallback 语义）。
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	conf "dugu-hr-saas-admin/config"
)

var _client *redis.Client

// InitCache 建立 redis 客户端并探活。出错返回 error，不再内部 panic。
func InitCache() error {
	cfg := conf.Config.Redis
	addr := cfg.RedisHost + ":" + cfg.RedisPort
	_client = redis.NewClient(&redis.Options{
		Addr:         addr,
		Network:      cfg.RedisNetwork,
		Username:     cfg.RedisUsername,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDbName,
		PoolSize:     20,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := _client.Ping(ctx).Err(); err != nil {
		_ = _client.Close()
		_client = nil
		return fmt.Errorf("连接 redis 失败(%s): %w", addr, err)
	}
	return nil
}

// Close 释放 redis 连接（优雅退出时调用）。
func Close() error {
	if _client == nil {
		return nil
	}
	err := _client.Close()
	_client = nil
	return err
}

// Client 获取共享客户端（service/middleware 使用）。
func Client() *redis.Client { return _client }
