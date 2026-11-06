package cache

import (
	"time"

	"dugu-hr-saas-admin/consts"
)

// ---------- 失败计数与锁定（Redis 加速层；DB 兜底在 dao.SysUserDao） ----------

// GetFailCount 用户维度失败计数。
func GetFailCount(uid int64) (int, error) {
	n, err := Client().Get(ctxBG, consts.KeyFailCount+itoa(uid)).Int()
	if isNilErr(err) {
		return 0, nil
	}
	return n, err
}

// IncrFail 失败计数 +1，窗口与锁定时长一致（锁到期窗口自然过期）。
func IncrFail(uid int64) (int, error) {
	key := consts.KeyFailCount + itoa(uid)
	pipe := Client().Pipeline()
	incr := pipe.Incr(ctxBG, key)
	pipe.Expire(ctxBG, key, consts.LockDuration)
	if _, err := pipe.Exec(ctxBG); err != nil {
		return 0, err
	}
	return int(incr.Val()), nil
}

// ClearFail 成功登录清零。
func ClearFail(uid int64) error {
	return Client().Del(ctxBG, consts.KeyFailCount+itoa(uid)).Err()
}

// SetFailLock 写锁定标记，TTL 即剩余锁定秒数（锁定期内尝试不重置计时，靠固定 TTL）。
func SetFailLock(uid int64) error {
	return Client().Set(ctxBG, consts.KeyFailLock+itoa(uid), 1, consts.LockDuration).Err()
}

// ClearFailLock 解锁时清除锁定标记与计数。
func ClearFailLock(uid int64) error {
	return Client().Del(ctxBG,
		consts.KeyFailLock+itoa(uid),
		consts.KeyFailCount+itoa(uid)).Err()
}

// LockRemainingSecs 锁定的剩余秒数（docs §10：拒绝并返回 remaining_seconds）。
func LockRemainingSecs(uid int64) (int64, error) {
	ttl, err := Client().TTL(ctxBG, consts.KeyFailLock+itoa(uid)).Result()
	if err != nil {
		return 0, err
	}
	if ttl <= 0 {
		return 0, nil
	}
	return int64(ttl / time.Second), nil
}

// ---------- IP 维度限流（docs §6/§8.6） ----------

// IPLoginAllowed 登录接口 IP 限流：20 次/分钟。
func IPLoginAllowed(ip string) (bool, error) {
	n, err := incrWindow(consts.KeyLoginIPLimit+ip, time.Minute)
	return n <= consts.LoginIPLimitMinute, err
}

// IPFailAllowed IP 撞库兜底：50 次/小时。
func IPFailAllowed(ip string) (bool, error) {
	n, err := incrWindow(consts.KeyIPFail+ip, time.Hour)
	return n <= consts.IPFailLimitHour, err
}

// IPFailIncr 撞库计数 +1（每次登录尝试判定后写入，含成功可计入总量）。
func IPFailIncr(ip string) error {
	_, err := incrWindow(consts.KeyIPFail+ip, time.Hour)
	return err
}

// incrWindow 固定窗口计数：INCR + EXPIRE。
func incrWindow(key string, window time.Duration) (int64, error) {
	pipe := Client().Pipeline()
	incr := pipe.Incr(ctxBG, key)
	pipe.Expire(ctxBG, key, window)
	if _, err := pipe.Exec(ctxBG); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// ---------- 连续失败步数（触发图形验证码，docs §8.7） ----------

// 是“连续”失败：成功登录即清零；锁定后的尝试按 docs §10 不重复计入（仍保留步数）。

// StreakUID 维度步数（账号存在时的 user_id 键）。
func StreakUID(uid int64) (int, error) {
	n, err := Client().Get(ctxBG, consts.KeyLoginStreakID+itoa(uid)).Int()
	if isNilErr(err) {
		return 0, nil
	}
	return n, err
}

// StreakIncrUID 步数 +1，1 小时窗口。
func StreakIncrUID(uid int64) (int, error) {
	n, err := incrWindow(consts.KeyLoginStreakID+itoa(uid), time.Hour)
	return int(n), err
}

// StreakKey 维度步数（账号不存在时的 identifier+IP 键，防止按标识计数攻击）。
func StreakKey(idMask, ip string) (int, error) {
	n, err := Client().Get(ctxBG, consts.KeyLoginStreakKey+idMask+"@"+ip).Int()
	if isNilErr(err) {
		return 0, nil
	}
	return n, err
}

func StreakKeyIncr(idMask, ip string) (int, error) {
	n, err := incrWindow(consts.KeyLoginStreakKey+idMask+"@"+ip, time.Hour)
	return int(n), err
}

// StreakReset 成功后清除两个维度的步数。
func StreakReset(uid int64, idMask, ip string) error {
	return Client().Del(ctxBG,
		consts.KeyLoginStreakID+itoa(uid),
		consts.KeyLoginStreakKey+idMask+"@"+ip).Err()
}
