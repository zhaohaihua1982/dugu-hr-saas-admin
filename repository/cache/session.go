package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"dugu-hr-saas-admin/consts"
)

// ErrSessionNotFound 会话不存在（登出/被秒踢/过期）。
var ErrSessionNotFound = errors.New("会话不存在")

var ctxBG = context.Background()

// CreateSession 登录成功后建会话键（docs §7）：
//   - sess:{jti}        → user_id，TTL 8h（每次请求滑动续期）
//   - sess:abs:{jti}    → user_id，TTL 24h（硬上限，不续期；防永远不过期）
//   - sess:u:{uid}      → SET(jti)，登出/秒踢全端用
//
// 返回空闲过期剩余 TTL（供前端展示），参数 jti 由 JWT 载荷提供。
func CreateSession(uid int64, jti string) error {
	c := Client()
	pipe := c.Pipeline()
	pipe.Set(ctxBG, consts.KeySession+jti, uid, consts.IdleSessionTTL)
	pipe.Set(ctxBG, consts.KeySessionAbs+jti, uid, consts.HardSessionTTL)
	pipe.SAdd(ctxBG, consts.KeySessionUser+itoa(uid), jti)
	// 集合成员个别的 jti 键会自然过期；给集合一个略长 TTL 兜底回收
	pipe.Expire(ctxBG, consts.KeySessionUser+itoa(uid), consts.HardSessionTTL+time.Minute)
	_, err := pipe.Exec(ctxBG)
	return err
}

// GetSessionUID 校验会话有效性：返回绑定的 uid（与 JWT 载荷比对防串号）。
func GetSessionUID(jti string) (int64, error) {
	c := Client()
	id, err := c.Get(ctxBG, consts.KeySession+jti).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, ErrSessionNotFound
	}
	if err != nil {
		return 0, err
	}
	// 硬上限键缺失（已过 24h）立即失效
	if exist, err := c.Exists(ctxBG, consts.KeySessionAbs+jti).Result(); err != nil {
		return 0, err
	} else if exist == 0 {
		return 0, ErrSessionNotFound
	}
	return id, nil
}

// SlideSession 滑动续期：每次认证请求都把 sess:{jti} 续到 8h。
// 续期前必须确认 hard-cap 键仍在；hard-cap 到期则整个会话作废。
func SlideSession(jti string) error {
	c := Client()
	exist, err := c.Exists(ctxBG, consts.KeySessionAbs+jti).Result()
	if err != nil {
		return err
	}
	if exist == 0 {
		return ErrSessionNotFound
	}
	return c.Expire(ctxBG, consts.KeySession+jti, consts.IdleSessionTTL).Err()
}

// DeleteSession 登出：删会话键并从用户集合移除。
func DeleteSession(uid int64, jti string) error {
	c := Client()
	pipe := c.Pipeline()
	pipe.Del(ctxBG, consts.KeySession+jti, consts.KeySessionAbs+jti)
	pipe.SRem(ctxBG, consts.KeySessionUser+itoa(uid), jti)
	_, err := pipe.Exec(ctxBG)
	return err
}

// KickUserSessions 改密码/停用账号 → 删除该 user 全部会话键，全端秒踢（docs §7）。
func KickUserSessions(uid int64) error {
	c := Client()
	key := consts.KeySessionUser + itoa(uid)
	jtis, err := c.SMembers(ctxBG, key).Result()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(jtis)*2)
	for _, jti := range jtis {
		keys = append(keys, consts.KeySession+jti, consts.KeySessionAbs+jti)
	}
	pipe := c.Pipeline()
	if len(keys) > 0 {
		pipe.Del(ctxBG, keys...)
	}
	pipe.Del(ctxBG, key)
	_, err = pipe.Exec(ctxBG)
	return err
}
