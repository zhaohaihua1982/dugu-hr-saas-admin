// authn.SessionStore 的 Redis 实现（docs §7）。
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/sysuser"
)

// 编译期断言：实现全部被消费端口。
var (
	_ authn.SessionStore = (*SessionStore)(nil)
)

// SessionStore 会话键管理。
type SessionStore struct{ c *Client }

// NewSessionStore 构造。
func NewSessionStore(c *Client) *SessionStore { return &SessionStore{c} }

// Create 放置 sess:{jti}（8h 滑动 + 24h 硬上限）与用户集合索引。
func (s *SessionStore) Create(ctx context.Context, uid int64, jti string) error {
	pipe := s.c.raw().Pipeline()
	pipe.Set(ctx, keySession+jti, uid, authn.IssueTokenTTL)
	pipe.Set(ctx, keySessionAbs+jti, uid, authn.HardSessionTTL)
	pipe.SAdd(ctx, keySessionUser+itoa(uid), jti)
	pipe.Expire(ctx, keySessionUser+itoa(uid), authn.HardSessionTTL+time.Minute)
	_, err := pipe.Exec(ctx)
	return err
}

// Get 校验会话有效性：jti 键存在 + 硬上限键仍在。
func (s *SessionStore) Get(ctx context.Context, jti string) (int64, error) {
	id, err := s.c.raw().Get(ctx, keySession+jti).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, authn.ErrSessionNotFound
	}
	if err != nil {
		return 0, err
	}
	exist, err := s.c.raw().Exists(ctx, keySessionAbs+jti).Result()
	if err != nil {
		return 0, err
	}
	if exist == 0 { // 24h 硬上限到期，整个会话作废
		return 0, authn.ErrSessionNotFound
	}
	return id, nil
}

// Slide 滑动续期（8h）；硬上限键缺失则拒绝。
func (s *SessionStore) Slide(ctx context.Context, jti string) error {
	exist, err := s.c.raw().Exists(ctx, keySessionAbs+jti).Result()
	if err != nil {
		return err
	}
	if exist == 0 {
		return authn.ErrSessionNotFound
	}
	return s.c.raw().Expire(ctx, keySession+jti, authn.IssueTokenTTL).Err()
}

// Delete 登出当前会话。
func (s *SessionStore) Delete(ctx context.Context, uid int64, jti string) error {
	pipe := s.c.raw().Pipeline()
	pipe.Del(ctx, keySession+jti, keySessionAbs+jti)
	pipe.SRem(ctx, keySessionUser+itoa(uid), jti)
	_, err := pipe.Exec(ctx)
	return err
}

// KickAll 改密码/停用账号 → 全端秒踢（docs §7）。
func (s *SessionStore) KickAll(ctx context.Context, uid int64) error {
	key := keySessionUser + itoa(uid)
	jtis, err := s.c.raw().SMembers(ctx, key).Result()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(jtis)*2)
	for _, jti := range jtis {
		keys = append(keys, keySession+jti, keySessionAbs+jti)
	}
	pipe := s.c.raw().Pipeline()
	if len(keys) > 0 {
		pipe.Del(ctx, keys...)
	}
	pipe.Del(ctx, key)
	_, err = pipe.Exec(ctx)
	return err
}

// ---------- 一次性验证码存储 ----------

// CaptchaService authn.CaptchaService 实现（Redis 一次性答案）。
type CaptchaService struct {
	c      *Client
	newOne func() (id, answer, dataURL string, err error) // SVG 生成器（infrastructure 注入）
}

// NewCaptchaService 构造（generator 来自 infrastructure/captcha）。
func NewCaptchaService(c *Client, generator func() (string, string, string, error)) *CaptchaService {
	return &CaptchaService{c: c, newOne: generator}
}

var _ authn.CaptchaService = (*CaptchaService)(nil)

// Issue 生成并存答案（TTL 5 分钟）。
func (p *CaptchaService) Issue(ctx context.Context) (authn.Captcha, error) {
	id, answer, img, err := p.newOne()
	if err != nil {
		return authn.Captcha{}, err
	}
	if err := p.c.raw().Set(ctx, keyCaptcha+id, answer, captchaTTL()).Err(); err != nil {
		return authn.Captcha{}, err
	}
	return authn.Captcha{ID: id, Image: img}, nil
}

// Verify 一次性：GetDel（用后即删，防重放）。
func (p *CaptchaService) Verify(ctx context.Context, id, answer string) (bool, error) {
	if id == "" || answer == "" {
		return false, nil
	}
	expect, err := p.c.raw().GetDel(ctx, keyCaptcha+id).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 过期或已用
	}
	if err != nil {
		return false, err
	}
	return expect == answer, nil
}

// ---------- 短信验证码（docs §5：5 分钟、一次性、试错 5 次作废、用途隔离） ----------

var _ authn.SmsCodeStore = (*SmsCodeStore)(nil)

// SmsCodeStore 短信验证码存储。
type SmsCodeStore struct{ c *Client }

// NewSmsCodeStore 构造。
func NewSmsCodeStore(c *Client) *SmsCodeStore { return &SmsCodeStore{c} }

// Save 存 HASH{code,tries}，TTL 5 分钟；key 带用途（登录码不能用于改密）。
func (s *SmsCodeStore) Save(ctx context.Context, p authn.SmsPurpose, mobileHash, code string) error {
	return s.c.raw().HSet(ctx, smsCodeKey(p, mobileHash),
		map[string]interface{}{"code": code, "tries": 0}).Err()
}

// Verify 一次性校验；错满 MaxTries 即作废（返回 CodeExhausted 引导重发）。
func (s *SmsCodeStore) Verify(ctx context.Context, p authn.SmsPurpose, mobileHash, code string) (authn.SmsCodeResult, error) {
	key := smsCodeKey(p, mobileHash)
	val, err := s.c.raw().HGet(ctx, key, "code").Result()
	if errors.Is(err, redis.Nil) {
		return authn.CodeNoCode, nil
	}
	if err != nil {
		return authn.CodeNoCode, err
	}
	if val == code {
		if derr := s.c.raw().Del(ctx, key).Err(); derr != nil { // 用后即删（防重放）
			return authn.CodeNoCode, derr
		}
		return authn.CodeOK, nil
	}
	tries, err := s.c.raw().HIncrBy(ctx, key, "tries", 1).Result()
	if err != nil {
		return authn.CodeMismatch, err
	}
	if int(tries) >= maxSmsTries() {
		if derr := s.c.raw().Del(ctx, key).Err(); derr != nil {
			return authn.CodeExhausted, derr
		}
		return authn.CodeExhausted, nil
	}
	return authn.CodeMismatch, nil
}

// ---------- 风控端口实现 ----------

var (
	_ authn.LoginGuard = (*LoginGuard)(nil)
	_ authn.LockGuard  = (*LockGuard)(nil)
)

// LoginGuard IP 维度限流/撞库/短信配额。
type LoginGuard struct{ c *Client }

// NewLoginGuard 构造。
func NewLoginGuard(c *Client) *LoginGuard { return &LoginGuard{c} }

// AllowLogin 登录接口 IP 限制：20 次/分钟。
func (g *LoginGuard) AllowLogin(ctx context.Context, ip string) error {
	n, err := incrWindow(g.c, ctx, keyLoginIPLimit+ip, time.Minute)
	if err != nil {
		return err
	}
	if n > authn.LoginRateLimit {
		return authn.ErrRateLimited
	}
	return nil
}

// AllowIPFail IP 撞库兜底：50 次/小时。
func (g *LoginGuard) AllowIPFail(ctx context.Context, ip string) error {
	n, err := incrWindow(g.c, ctx, keyIPFail+ip, time.Hour)
	if err != nil {
		return err
	}
	if n > authn.IPFailLimitHour {
		return authn.ErrRateLimited
	}
	return nil
}

// RecordIPFail IP 撞库计数恒写入（ Portu 语义：账号/不存在都计入）。
func (g *LoginGuard) RecordIPFail(ctx context.Context, ip string) error {
	_, err := incrWindow(g.c, ctx, keyIPFail+ip, time.Hour)
	return err
}

// AllowSms 配额：单手机 1/min、10/day；单 IP 100/day（docs §5）。
func (g *LoginGuard) AllowSms(ctx context.Context, p authn.SmsPurpose, mobileHash, ip string) error {
	quota := []struct {
		key    string
		window time.Duration
		max    int64
	}{
		{smsMinuteKey(p, mobileHash), minuteLimitWindow, minuteLimitMax},
		{smsDayKey(p, mobileHash), dayLimitWindow, dailyLimitMax},
		{smsIPDayKey(ip), dayLimitWindow, ipDailyLimitMax},
	}
	for _, q := range quota {
		n, err := incrWindow(g.c, ctx, q.key, q.window)
		if err != nil {
			return err
		}
		if n > q.max {
			return authn.ErrRateLimited
		}
	}
	return nil
}

// incrWindow 固定窗口 INCR+EXPIRE。
func incrWindow(c *Client, ctx context.Context, key string, window time.Duration) (int64, error) {
	pipe := c.raw().Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

// LockGuard user 维度失败计数与锁定（Redis 加速层）。
type LockGuard struct{ c *Client }

// NewLockGuard 构造。
func NewLockGuard(c *Client) *LockGuard { return &LockGuard{c} }

// Remaining 锁定剩余时长；未锁定 0。Redis 故障原样上抛（绝不因故障放行）。
func (l *LockGuard) Remaining(ctx context.Context, uid int64) (time.Duration, error) {
	ttl, err := l.c.raw().TTL(ctx, keyFailLock+itoa(uid)).Result()
	if err != nil {
		return 0, err
	}
	if ttl <= 0 {
		return 0, nil
	}
	return ttl, nil
}

// IncrFail 计数 +1，窗口与锁定时长一致。
func (l *LockGuard) IncrFail(ctx context.Context, uid int64) (int, error) {
	key := keyFailCount + itoa(uid)
	pipe := l.c.raw().Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, sysuser.LockDuration)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return int(incr.Val()), nil
}

// MarkLocked 锁定标记，TTL=锁定时长。
func (l *LockGuard) MarkLocked(ctx context.Context, uid int64, d time.Duration) error {
	return l.c.raw().Set(ctx, keyFailLock+itoa(uid), 1, d).Err()
}

// ResetFail 成功后清零计数/锁定/双维 streak。
func (l *LockGuard) ResetFail(ctx context.Context, uid int64, idMask, ip string) error {
	return l.c.raw().Del(ctx,
		keyFailLock+itoa(uid),
		keyFailCount+itoa(uid),
		keyLoginStreakUID+itoa(uid),
		keyLoginStreakKey+idMask+"@"+ip).Err()
}

// IncrStreak uid 维度步数。
func (l *LockGuard) IncrStreak(ctx context.Context, uid int64) (int, error) {
	n, err := incrWindow(l.c, ctx, keyLoginStreakUID+itoa(uid), sysuser.LockDuration)
	return int(n), err
}

// GetStreak uid 维度步数读取。
func (l *LockGuard) GetStreak(ctx context.Context, uid int64) (int, error) {
	n, err := l.c.raw().Get(ctx, keyLoginStreakUID+itoa(uid)).Int()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return n, err
}

// IncrStreakByKey 账号不存在时的“标识+IP”兜底计数。
func (l *LockGuard) IncrStreakByKey(ctx context.Context, idMask, ip string) (int, error) {
	n, err := incrWindow(l.c, ctx, keyLoginStreakKey+idMask+"@"+ip, sysuser.LockDuration)
	return int(n), err
}

// itoa int64 → 装饰用途的字串。
func itoa(v int64) string { return fmtInt(v) }
