// Package cache —— authn 端口的 Redis 实现（会话/验证码/短信码/风控计数）。
// docs §6 铁律：Redis 是加速，DB 是兜底——端口实现把“键不存在”规范为业务
// 结果（ErrSessionNotFound/CodeNoCode），把连接故障原样上抛，由 application 决策。
package cache

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	conf "dugu-hr-saas-admin/config"
)

// Client redis 封装。
type Client struct{ rdb *redis.Client }

// Open 建立客户端并探活。
func Open() (*Client, error) {
	cfg := conf.Config.Redis
	addr := cfg.RedisHost + ":" + cfg.RedisPort
	rdb := redis.NewClient(&redis.Options{
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
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return &Client{rdb: rdb}, nil
}

// Close 释放连接。
func (c *Client) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

func (c *Client) raw() *redis.Client { return c.rdb }

// ---------- Redis key 规则（docs §5/§7） ----------

const (
	keySession        = "sess:"             // sess:{jti} → uid，TTL 8h 滑动
	keySessionAbs     = "sess:abs:"         // sess:abs:{jti} → 硬上限标记，TTL 24h
	keySessionUser    = "sess:u:"           // sess:u:{uid} → SET(jti)，秒踢全端
	keyCaptcha        = "captcha:"          // captcha:{id} → 答案，一次性
	keyLoginIPLimit   = "login:limit:ip:"   // IP 登录限流
	keyLoginStreakUID = "login:streak:uid:" // uid 维度连续失败
	keyLoginStreakKey = "login:streak:key:" // 标识+IP 兜底
	keyIPFail         = "login:ipfail:"     // IP 撞库计数
	keyFailCount      = "fail:cnt:"         // uid 失败计数
	keyFailLock       = "fail:lock:"        // 锁定标记（TTL=剩余）
)
