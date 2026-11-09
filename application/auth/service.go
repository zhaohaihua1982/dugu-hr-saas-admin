// Package auth —— 认证用例：账号密码/手机密码/手机短信三入口汇一管线（docs §4）。
// 管线铁律（docs §4.1）：解析只判存在 → 因子校验 → 状态判定 → 签发+清计数+日志。
package auth

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appsrv "dugu-hr-saas-admin/application/sysuser"
	"dugu-hr-saas-admin/application/types"
	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/loginlog"
	"dugu-hr-saas-admin/domain/sysuser"
)

// ---------- 用例输入/输出对象 ----------

// CaptchaGetResp 图形验证码响应。
type CaptchaGetResp struct {
	CaptchaID string `json:"captcha_id"`
	Image     string `json:"image"`
}

// SendSmsReq 发送登录短信（docs §9：需图形码）。
type SendSmsReq struct {
	Mobile      string `json:"mobile" binding:"required"`
	CaptchaID   string `json:"captcha_id" binding:"required"`
	CaptchaCode string `json:"captcha_code" binding:"required"`
}

// LoginPasswordReq 账号或手机 + 密码（CaptchaID/Code 连续失败达阈值后必带，docs §8.7）。
type LoginPasswordReq struct {
	Identifier  string `json:"identifier" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
}

// LoginSmsReq 手机 + 短信码。
type LoginSmsReq struct {
	Mobile string `json:"mobile" binding:"required"`
	Code   string `json:"code" binding:"required"`
}

// LoginResp 登录成功响应（token + 用户视图 + 改密提示）。
type LoginResp struct {
	Token         string          `json:"token"`
	ExpiresIn     int64           `json:"expires_in"`
	User          appsrv.UserView `json:"user"`
	PasswordStale bool            `json:"password_stale"`
}

// ---------- 用例 ----------

// Service 认证用例容器（全部依赖为 domain 端口，bootstrap 注入）。
type Service struct {
	users    sysuser.Repository
	logs     loginlog.Repository
	tokens   authn.TokenIssuer
	sessions authn.SessionStore
	captcha  authn.CaptchaService
	guard    authn.LoginGuard
	lock     authn.LockGuard
	codes    authn.SmsCodeStore
	sms      authn.SmsSender
	mobile   sysuser.MobileCrypto
	hasher   sysuser.PasswordHasher
	ids      authn.IDGenerator
}

// NewService 构造。
func NewService(
	users sysuser.Repository,
	logs loginlog.Repository,
	tokens authn.TokenIssuer,
	sessions authn.SessionStore,
	captcha authn.CaptchaService,
	guard authn.LoginGuard,
	lock authn.LockGuard,
	codes authn.SmsCodeStore,
	sms authn.SmsSender,
	mobile sysuser.MobileCrypto,
	hasher sysuser.PasswordHasher,
	ids authn.IDGenerator,
) *Service {
	return &Service{
		users: users, logs: logs, tokens: tokens, sessions: sessions,
		captcha: captcha, guard: guard, lock: lock, codes: codes,
		sms: sms, mobile: mobile, hasher: hasher, ids: ids,
	}
}

// GetCaptcha 获取图形验证码（一次性、5 分钟有效；签发有 IP 防刷配额）。
func (s *Service) GetCaptcha(ctx context.Context, p types.RequestParams) (*CaptchaGetResp, error) {
	// 防刷：白名单接口同样要限制拉取频率（10/min、60/h），否则白名单成免费放大器
	if err := s.guard.AllowCaptcha(ctx, p.IP); err != nil {
		return nil, err
	}
	c, err := s.captcha.Issue(ctx)
	if err != nil {
		return nil, err
	}
	return &CaptchaGetResp{CaptchaID: c.ID, Image: c.Image}, nil
}

// LoginPassword 账号或手机 + 密码（docs §4.2 入口一/二）。
func (s *Service) LoginPassword(ctx context.Context, req *LoginPasswordReq, p types.RequestParams, now time.Time) (*LoginResp, error) {
	ident := strings.TrimSpace(req.Identifier)

	// 输入快筛（docs：达标才进入管线）：账号长度/密码长度明显不合法的请求，
	// 直接判参数错误返回——不进 DB 查询、不进 Redis 计数、更不跑 bcrypt。
	// 判据与账号是否真实存在无关（对存在/不存在的账号同样拒绝），无枚举信号。
	if !sysuser.PlausibleIdentifier(ident) {
		return nil, fmt.Errorf("%w: identifier", sysuser.ErrBadUsername)
	}
	if !sysuser.PlausiblePassword(req.Password) {
		return nil, fmt.Errorf("%w: password", sysuser.ErrWeakPassword)
	}

	// IP 限流（20/min）与撞库兜底（50/h）
	if err := s.guard.AllowLogin(ctx, p.IP); err != nil {
		s.recordLog(0, "", loginlog.Password, "", false, loginlog.ReasonRateLimited, p)
		return nil, err
	}
	if err := s.guard.AllowIPFail(ctx, p.IP); err != nil {
		s.recordLog(0, "", loginlog.Password, "", false, loginlog.ReasonRateLimited, p)
		return nil, err
	}

	// ① 解析 user（只判存在）
	u, via, err := s.resolve(ctx, ident)
	if err != nil {
		return nil, err
	}
	idMask := sysuser.LogIdentifier(ident) // v1.2：账号原样、手机脱敏（docs §3.2）

	// ② 图形码校验（连续失败 3 次后必带，docs §8.7）
	if err := s.requireCaptcha(ctx, u, idMask, p, req.CaptchaID, req.CaptchaCode); err != nil {
		return nil, err
	}

	// ③ 因子校验（密码）；不存在账号陪跑假 hash（docs §8.1）
	if err := s.verifyPassword(ctx, u, req.Password); err != nil {
		return nil, s.failAttempt(ctx, u, p, loginlog.Password, via, idMask, loginlog.ReasonBadCredential)
	}

	// ④ 状态判定（在因子之后；停用不计失败次数，docs §6/§10）
	if u.IsDisabled() {
		s.recordLog(u.ID, idMask, loginlog.Password, via, false, loginlog.ReasonDisabled, p)
		return nil, authn.ErrBadCredential // 统一文案：账号或密码错误
	}
	if u.IsLocked(now) {
		s.recordLog(u.ID, idMask, loginlog.Password, via, false, loginlog.ReasonLocked, p)
		return nil, &authn.ErrLocked{Remaining: u.LockedRemaining(now)}
	}

	// ⑤ 签发会话 + 日志 + 清计数
	return s.success(ctx, u, loginlog.Password, via, idMask, p, now)
}

// LoginSms 手机 + 短信码（入口三；验证码通过后汇入状态管线）。
func (s *Service) LoginSms(ctx context.Context, req *LoginSmsReq, p types.RequestParams, now time.Time) (*LoginResp, error) {
	norm, err := sysuser.NormalizeMobile(req.Mobile)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", sysuser.ErrBadMobile, err)
	}
	if err := s.guard.AllowLogin(ctx, p.IP); err != nil {
		s.recordLog(0, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonRateLimited, p)
		return nil, err
	}

	u, err := s.users.ByMobileHash(ctx, s.mobile.Hash(norm)) // 归一化 11 位串不命中 username（§3.3③）
	if err != nil {
		return nil, err
	}

	// 因子：验证码——先做定长/字符集快筛（6 位数字），不达标不打 Redis 与试错计数
	if !validSmsCode(req.Code) {
		s.recordLog(0, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonSmsCodeError, p)
		return nil, authn.ErrBadCodeFormat
	}
	// 用途隔离在存储 key 内
	switch res, verr := s.codes.Verify(ctx, authn.PurposeLogin, s.mobile.Hash(norm), req.Code); {
	case verr != nil:
		return nil, verr
	case res == authn.CodeOK:
		// 汇入状态管线
	case res == authn.CodeNoCode, res == authn.CodeExhausted:
		// 一个口径：未注册/未发送/过期统一 SMS_EXPIRED，无存在性信号
		return nil, s.smsFailure(ctx, u, norm, p, loginlog.ReasonSmsExpired)
	case res == authn.CodeMismatch:
		return nil, s.smsFailure(ctx, u, norm, p, loginlog.ReasonSmsCodeError)
	}

	// 状态判定（同密码管线）
	if u.IsDisabled() {
		s.recordLog(u.ID, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonDisabled, p)
		return nil, authn.ErrBadCredential
	}
	if u.IsLocked(now) {
		s.recordLog(u.ID, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonLocked, p)
		return nil, &authn.ErrLocked{Remaining: u.LockedRemaining(now)}
	}

	return s.success(ctx, u, loginlog.SMS, sysuser.ViaMobile, sysuser.MaskMobile(norm), p, now)
}

// SendSms 发送登录短信（docs §5 全部规则）。
func (s *Service) SendSms(ctx context.Context, req *SendSmsReq, p types.RequestParams) error {
	// 前置图形码（短信轰炸是真实资损）；答案先过格式快筛再碰 Redis
	if !plausibleCaptchaCode(req.CaptchaCode) {
		return authn.ErrCaptchaFailed
	}
	ok, err := s.captcha.Verify(ctx, req.CaptchaID, req.CaptchaCode)
	if err != nil {
		return err
	}
	if !ok {
		return authn.ErrCaptchaFailed
	}

	norm, err := sysuser.NormalizeMobile(req.Mobile)
	if err != nil {
		return fmt.Errorf("%w: %v", sysuser.ErrBadMobile, err)
	}
	if err := s.guard.AllowSms(ctx, authn.PurposeLogin, s.mobile.Hash(norm), p.IP); err != nil {
		return err
	}

	// 防枚举（docs §5）：查到人才真正发；查不到 → 返回“已发送”，日志记 SEND_SKIPPED
	u, err := s.users.ByMobileHash(ctx, s.mobile.Hash(norm))
	if err != nil {
		return err
	}
	if u == nil {
		s.recordLog(0, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonSendSkipped, p)
		return nil
	}

	plain, err := s.mobile.Decrypt(u.Mobile.Enc)
	if err != nil {
		return fmt.Errorf("%w: %v", authn.ErrSmsSendFailed, err)
	}
	code := smsCode()
	if err := s.codes.Save(ctx, authn.PurposeLogin, s.mobile.Hash(norm), code); err != nil {
		return err
	}
	if err := s.sms.Send(ctx, plain, code); err != nil {
		return fmt.Errorf("%w: %v", authn.ErrSmsSendFailed, err)
	}
	return nil
}

// Logout 登出当前会话。
func (s *Service) Logout(ctx context.Context, uid int64, jti string) error {
	return s.sessions.Delete(ctx, uid, jti)
}

// Me 当前用户信息（视图复用账号域 DTO，避免同层重复定义）。
func (s *Service) Me(ctx context.Context, uid int64, now time.Time) (*appsrv.UserView, bool, error) {
	u, err := s.users.ByID(ctx, uid)
	if err != nil {
		return nil, false, err
	}
	if u == nil {
		return nil, false, authn.ErrBadCredential
	}
	v := appsrv.ToView(u)
	return &v, u.PasswordStale(now), nil
}

// ---------- 管线内部 ----------

// resolve 登录标识解析（领域服务 ResolveIdentifier，docs §3.3③）。
func (s *Service) resolve(ctx context.Context, ident string) (*sysuser.User, sysuser.Via, error) {
	r, err := sysuser.ResolveIdentifier(ctx, ident, s.users, s.mobile)
	if err != nil {
		return nil, "", err
	}
	if r == nil {
		return nil, "", nil
	}
	return r.User, r.Via, nil
}

// requireCaptcha 图形码守卫：连续失败 ≥ StreakThreshold 后必带（docs §8.7）。
func (s *Service) requireCaptcha(ctx context.Context, u *sysuser.User, idMask string, p types.RequestParams, captID, captCode string) error {
	if u == nil {
		return nil // 不存在账号交给统一 BAD_CREDENTIAL 口径
	}
	streak, err := s.lock.GetStreak(ctx, u.ID)
	if err != nil {
		slog.Warn("读取连续失败步数失败（保守放行，DB 锁定仍兜底）", "user_id", u.ID, "err", err)
		return nil
	}
	if streak < sysuser.StreakThreshold {
		return nil
	}
	if captID == "" || captCode == "" {
		return authn.ErrCaptchaRequired
	}
	if !plausibleCaptchaCode(captCode) { // 图形码答案定长 5 位数字，格式不对不必碰 Redis
		return authn.ErrCaptchaFailed
	}
	ok, err := s.captcha.Verify(ctx, captID, captCode)
	if err != nil {
		return err
	}
	if !ok {
		_ = s.failAttempt(ctx, u, p, loginlog.Password, "", idMask, loginlog.ReasonCaptchaFailed)
		return authn.ErrCaptchaFailed
	}
	return nil
}

// verifyPassword 因子校验（bcrypt 主路；sha256 旧算法透明升级，docs §3.3②）。
func (s *Service) verifyPassword(ctx context.Context, u *sysuser.User, pwd string) error {
	if u == nil {
		s.hasher.Decoy(pwd) // 陪跑假 hash 抹平时序差（docs §8.1）
		return authn.ErrBadCredential
	}
	if !u.Credential.HasPassword() {
		return authn.ErrBadCredential // 无密码：仅短信登录
	}
	ok, upgrade, err := u.Credential.Verify(s.hasher, pwd)
	if err != nil {
		return err
	}
	if !ok {
		return authn.ErrBadCredential
	}
	if upgrade != nil { // 透明升级：旧算法迁移 bcrypt（docs §3.3②）
		if uerr := s.users.UpdatePassword(ctx, u.ID, *upgrade); uerr != nil {
			slog.Warn("旧密码透明升级失败", "user_id", u.ID, "err", uerr)
		} else {
			slog.Info("旧算法密码已升级为 bcrypt", "user_id", u.ID)
		}
	}
	return nil
}

// failAttempt 失败计数 + 锁定 + 日志（docs §6：user_id 主维度 + IP 兜底；
// 不存在账号按“标识+IP”兜底计数，docs §10）。
func (s *Service) failAttempt(ctx context.Context, u *sysuser.User, p types.RequestParams, t loginlog.Type, via sysuser.Via, idMask string, reason loginlog.FailReason) error {
	if u == nil {
		_, _ = s.lock.IncrStreakByKey(ctx, idMask, p.IP)
	} else {
		n, rerr := s.lock.IncrFail(ctx, u.ID)
		if rerr != nil {
			slog.Warn("Redis 失败计数失败（DB 兜底仍执行）", "user_id", u.ID, "err", rerr)
			n = 0
		}
		if _, serr := s.lock.IncrStreak(ctx, u.ID); serr != nil {
			slog.Warn("连续失败步数写入失败", "user_id", u.ID, "err", serr)
		}

		cnt, derr := s.users.IncrFailures(ctx, u.ID, time.Now())
		personal := cnt
		if derr != nil {
			slog.Warn("DB 失败计数失败", "user_id", u.ID, "err", derr)
			personal, cnt = n, 0
		}
		if personal >= sysuser.FailThreshold {
			if lerr := s.users.MarkLocked(ctx, u.ID, time.Now().Add(sysuser.LockDuration)); lerr != nil {
				slog.Warn("DB 写锁定失败", "user_id", u.ID, "err", lerr)
			}
			if merr := s.lock.MarkLocked(ctx, u.ID, sysuser.LockDuration); merr != nil {
				slog.Warn("Redis 写锁定标记失败（DB locked_until 已生效）", "user_id", u.ID, "err", merr)
			}
		}
	}

	if rerr := s.guard.RecordIPFail(ctx, p.IP); rerr != nil {
		slog.Warn("IP 撞库计数失败", "ip", p.IP, "err", rerr)
	}

	s.recordLog(uidOf(u), idMask, t, via, false, reason, p)
	return authn.ErrBadCredential
}

// smsFailure 短信登录失败路径（统一口径；账号存在时计数）。
func (s *Service) smsFailure(ctx context.Context, u *sysuser.User, norm string, p types.RequestParams, reason loginlog.FailReason) error {
	if u == nil {
		// 未注册手机号：无 user 可计数，只记日志（user_id=0），口径与注册号一致
		s.recordLog(0, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, reason, p)
		if reason == loginlog.ReasonSmsExpired {
			return authn.ErrSmsExpired
		}
		return authn.ErrSmsCodeError
	}
	return s.failAttempt(ctx, u, p, loginlog.SMS, sysuser.ViaMobile, sysuser.MaskMobile(norm), reason)
}

// success 成功路径：签发会话 + 清计数 + 成功日志 + 更新最近登录。
func (s *Service) success(ctx context.Context, u *sysuser.User, t loginlog.Type, via sysuser.Via, idMask string, p types.RequestParams, now time.Time) (*LoginResp, error) {
	tok, err := s.tokens.Issue(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Create(ctx, u.ID, tok.JTI); err != nil {
		return nil, err
	}

	if err := s.users.SaveLoginSuccess(ctx, u.ID, p.IP, now); err != nil {
		slog.Warn("更新最近登录失败", "user_id", u.ID, "err", err)
	}
	if err := s.lock.ResetFail(ctx, u.ID, idMask, p.IP); err != nil {
		slog.Warn("清除失败计数失败", "user_id", u.ID, "err", err)
	}

	s.recordLog(u.ID, idMask, t, via, true, "", p)

	return &LoginResp{
		Token:         tok.Value,
		ExpiresIn:     int64(tok.TTL / time.Second),
		User:          appsrv.ToView(u),
		PasswordStale: u.PasswordStale(now),
	}, nil
}

// recordLog 登录日志：**写事务外**（独立 goroutine + 独立 ctx），
// 失败只告警不影响登录结果（docs §3.2 规则 1、§10）。
func (s *Service) recordLog(userID int64, identMask string, t loginlog.Type, via sysuser.Via, ok bool, reason loginlog.FailReason, p types.RequestParams) {
	go func(p types.RequestParams) {
		defer func() {
			// 日志写失败/异常绝不能拖垮登录主流程
			if r := recover(); r != nil {
				slog.Warn("登录日志异步写入 panic（已恢复，需告警）", "panic", r)
			}
		}()
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		w := &loginlog.Log{
			ID:             s.ids.New(),
			UserID:         userID,
			IdentifierMask: identMask,
			Type:           t,
			Via:            loginlog.Via(via),
			Success:        ok,
			Reason:         reason,
			IP:             p.IP,
			UA:             truncate(p.UA, 512),
		}
		if err := s.logs.Append(bg, w); err != nil {
			slog.Warn("登录日志写库失败（需监控告警）", "err", err)
		}
	}(p)
}

// validSmsCode 短信码格式：恰为 SmsCodeLength 位纯数字。
func validSmsCode(code string) bool {
	if len(code) != authn.SmsCodeLength {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// plausibleCaptchaCode 图形码答案格式（5 位数字）。
func plausibleCaptchaCode(code string) bool {
	return validSmsCodeConsistent(code, svcCaptchaLen)
}

const svcCaptchaLen = 5

func validSmsCodeConsistent(code string, want int) bool {
	if len(code) != want {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// smsCode 生成短信验证码（6 位数字，crypto/rand；docs §5）。
func smsCode() string {
	const digits = "0123456789"
	out := make([]byte, 6)
	for i := range out {
		b := make([]byte, 1)
		if _, err := crand.Read(b); err != nil {
			out[i] = digits[i%len(digits)] // crypto/rand 失败极罕见；退化避免 panic
			continue
		}
		out[i] = digits[int(b[0])%len(digits)]
	}
	return string(out)
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func uidOf(u *sysuser.User) int64 {
	if u == nil {
		return 0
	}
	return u.ID
}
