// 认证用例：三个入口（账号密码/手机密码/手机短信）在“解析”之后汇入同一条管线。
// 管线三条铁律（docs §4.1）：
//  1. 先解析 user（只判存在），不下状态结论；
//  2. 因子校验在状态判定（停用/锁定）之前——不给无凭证者确认账号状态的机会；
//  3. 失败计数按 user_id 计，不按标识分桶。
package application

import (
	"context"
	crand "crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/loginlog"
	"dugu-hr-saas-admin/domain/sysuser"
)

// AuthService 认证用例容器（端口由 bootstrap 注入）。
// 依赖倒置：本层只见 domain 接口，不见 SSH/GORM/Redis/JWT 具体实现。
type AuthService struct {
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

// NewAuthService 构造（所有依赖为 domain 端口）。
func NewAuthService(
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
) *AuthService {
	return &AuthService{
		users: users, logs: logs, tokens: tokens, sessions: sessions,
		captcha: captcha, guard: guard, lock: lock, codes: codes,
		sms: sms, mobile: mobile, hasher: hasher, ids: ids,
	}
}

// ---------- 图形验证码 ----------

// GetCaptcha 获取图形验证码（一次性、5 分钟有效）。
func (s *AuthService) GetCaptcha(ctx context.Context) (*CaptchaGetResp, error) {
	c, err := s.captcha.Issue(ctx)
	if err != nil {
		return nil, err
	}
	return &CaptchaGetResp{CaptchaID: c.ID, Image: c.Image}, nil
}

// ---------- 密码登录（docs §4.2 入口一/二：账号或手机 + 密码） ----------

func (s *AuthService) LoginPassword(ctx context.Context, req *LoginPasswordReq, p RequestParams, now time.Time) (*LoginResp, error) {
	ident := strings.TrimSpace(req.Identifier)

	// IP 限流（20/min）与撞库兜底（50/h）
	if err := s.guard.AllowLogin(ctx, p.IP); err != nil {
		s.recordLog(ctx, 0, "", loginlog.Password, "", false, loginlog.ReasonRateLimited, p)
		return nil, err
	}
	if err := s.guard.AllowIPFail(ctx, p.IP); err != nil {
		s.recordLog(ctx, 0, "", loginlog.Password, "", false, loginlog.ReasonRateLimited, p)
		return nil, err
	}

	// ① 解析 user（只判存在）
	u, via, err := s.resolve(ctx, ident)
	if err != nil {
		return nil, err
	}
	idMask := sysuser.MaskIdentifier(ident)

	// ② 图形码校验（连续失败 3 次后必带，docs §8.7）
	if err := s.requireCaptcha(ctx, u, ident, idMask, p, req.CaptchaID, req.CaptchaCode); err != nil {
		return nil, err
	}

	// ③ 因子校验（密码）；不存在账号陪跑假 hash（docs §8.1）
	if err := s.verifyPassword(ctx, u, req.Password); err != nil {
		return nil, s.failAttempt(ctx, u, p, loginlog.Password, via, idMask, loginlog.ReasonBadCredential)
	}

	// ④ 状态判定（在因子之后；停用不计失败次数，docs §6/§10）
	if u.IsDisabled() {
		s.recordLog(ctx, u.ID, idMask, loginlog.Password, via, false, loginlog.ReasonDisabled, p)
		return nil, authn.ErrBadCredential // 统一文案：账号或密码错误
	}
	if u.IsLocked(now) {
		s.recordLog(ctx, u.ID, idMask, loginlog.Password, via, false, loginlog.ReasonLocked, p)
		return nil, &authn.ErrLocked{Remaining: u.LockedRemaining(now)}
	}

	// ⑤ 签发会话 + 日志 + 清计数
	return s.success(ctx, u, loginlog.Password, via, idMask, p, now)
}

// ---------- 短信登录（入口三：手机 + 短信码） ----------

func (s *AuthService) LoginSms(ctx context.Context, req *LoginSmsReq, p RequestParams, now time.Time) (*LoginResp, error) {
	norm, err := sysuser.NormalizeMobile(req.Mobile)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", sysuser.ErrBadMobile, err)
	}
	if err := s.guard.AllowLogin(ctx, p.IP); err != nil {
		s.recordLog(ctx, 0, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonRateLimited, p)
		return nil, err
	}

	mHash := s.mobile.Hash(norm)
	u, err := s.users.ByMobileHash(ctx, mHash) // 归一化的 11 位串不会命中 username（不变式 §3.3③）
	if err != nil {
		return nil, err
	}

	// 因子：验证码（用途隔离在存储 key 内）
	switch res, verr := s.codes.Verify(ctx, authn.PurposeLogin, mHash, req.Code); {
	case verr != nil:
		return nil, verr
	case res == authn.CodeOK:
		// 汇入状态管线
	case res == authn.CodeNoCode, res == authn.CodeExhausted:
		// 一个口径：无存在性信号——未注册/未发送/过期统一 SMS_EXPIRED
		return nil, s.smsFailure(ctx, u, norm, p, loginlog.ReasonSmsExpired)
	case res == authn.CodeMismatch:
		return nil, s.smsFailure(ctx, u, norm, p, loginlog.ReasonSmsCodeError)
	}

	// 状态判定（同密码管线）
	if u.IsDisabled() {
		s.recordLog(ctx, u.ID, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonDisabled, p)
		return nil, authn.ErrBadCredential
	}
	if u.IsLocked(now) {
		s.recordLog(ctx, u.ID, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonLocked, p)
		return nil, &authn.ErrLocked{Remaining: u.LockedRemaining(now)}
	}

	return s.success(ctx, u, loginlog.SMS, sysuser.ViaMobile, sysuser.MaskMobile(norm), p, now)
}

// smsFailure 短信登录失败路径（统一返回；账号存在时计数）。
func (s *AuthService) smsFailure(ctx context.Context, u *sysuser.User, norm string, p RequestParams, reason loginlog.FailReason) error {
	if u == nil {
		// 未注册手机号：无 user 可计数，只记日志（user_id=0），口径与注册号一致
		s.recordLog(ctx, 0, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, reason, p)
		if reason == loginlog.ReasonSmsExpired {
			return authn.ErrSmsExpired
		}
		return authn.ErrSmsCodeError
	}
	return s.failAttempt(ctx, u, p, loginlog.SMS, sysuser.ViaMobile, sysuser.MaskMobile(norm), reason)
}

// ---------- 发送短信（docs §5 全部规则） ----------

func (s *AuthService) SendSms(ctx context.Context, req *SendSmsReq, p RequestParams) error {
	// 前置图形码（短信轰炸是真实资损）
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
		s.recordLog(ctx, 0, sysuser.MaskMobile(norm), loginlog.SMS, sysuser.ViaMobile, false, loginlog.ReasonSendSkipped, p)
		return nil
	}

	plain, err := s.mobile.Decrypt(u.Mobile.Enc)
	if err != nil {
		return fmt.Errorf("%w: %v", authn.ErrSmsSendFailed, err)
	}
	code := randDigits(6)
	if err := s.codes.Save(ctx, authn.PurposeLogin, s.mobile.Hash(norm), code); err != nil {
		return err
	}
	if err := s.sms.Send(ctx, plain, code); err != nil {
		return fmt.Errorf("%w: %v", authn.ErrSmsSendFailed, err)
	}
	return nil
}

// ---------- 登出 / 当前用户 ----------

// Logout 登出当前会话。
func (s *AuthService) Logout(ctx context.Context, uid int64, jti string) error {
	return s.sessions.Delete(ctx, uid, jti)
}

// Me 当前用户信息。
func (s *AuthService) Me(ctx context.Context, uid int64, now time.Time) (*UserView, bool, error) {
	u, err := s.users.ByID(ctx, uid)
	if err != nil {
		return nil, false, err
	}
	if u == nil {
		return nil, false, authn.ErrBadCredential
	}
	v := toUserView(u)
	return &v, u.PasswordStale(now), nil
}

// ---------- 管线内部 ----------

// resolve 登录标识解析（领域服务 ResolveIdentifier）。
func (s *AuthService) resolve(ctx context.Context, ident string) (*sysuser.User, sysuser.Via, error) {
	r, err := sysuser.ResolveIdentifier(ctx, ident, s.users, s.mobile)
	if err != nil {
		return nil, "", err
	}
	if r == nil {
		return nil, "", nil
	}
	return r.User, r.Via, nil
}

// requireCaptcha 图形码守卫：连续失败 ≥ sysuser.StreakThreshold 后必带。
//   - 未提交 → ErrCaptchaRequired（不计失败）；
//   - 提交但错 → 按 CAPTCHA_FAILED 计数与记账（docs §10）。
func (s *AuthService) requireCaptcha(ctx context.Context, u *sysuser.User, ident, idMask string, p RequestParams, captID, captCode string) error {
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
func (s *AuthService) verifyPassword(ctx context.Context, u *sysuser.User, pwd string) error {
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
	// 透明升级：校验成功即把旧算法迁移到 bcrypt（salt 清空）
	if upgrade != nil {
		if uerr := s.users.UpdatePassword(ctx, u.ID, *upgrade); uerr != nil {
			slog.Warn("旧密码透明升级失败", "user_id", u.ID, "err", uerr)
		} else {
			slog.Info("旧算法密码已升级为 bcrypt", "user_id", u.ID)
		}
	}
	return nil
}

// failAttempt 失败计数 + 锁定 + 日志（docs §6：user_id 主维度 + IP 兜底）。
// 不存在账号（u==nil）按“标识+IP”在 Redis 兜底计数（docs §10）。
func (s *AuthService) failAttempt(ctx context.Context, u *sysuser.User, p RequestParams, t loginlog.Type, via sysuser.Via, idMask string, reason loginlog.FailReason) error {
	if u == nil {
		_, _ = s.lock.IncrStreakByKey(ctx, idMask, p.IP)
	} else {
		// Redis 计数（加速层；出错不中断，DB 双写兜底）
		n, rerr := s.lock.IncrFail(ctx, u.ID)
		if rerr != nil {
			slog.Warn("Redis 失败计数失败（DB 兜底仍执行）", "user_id", u.ID, "err", rerr)
			n = 0
		}
		if _, serr := s.lock.IncrStreak(ctx, u.ID); serr != nil {
			slog.Warn("连续失败步数写入失败", "user_id", u.ID, "err", serr)
		}

		// DB 双写（Redis 是加速，DB 是兜底）
		cnt, derr := s.users.IncrFailures(ctx, u.ID, time.Now())
		personal := cnt
		if derr != nil {
			slog.Warn("DB 失败计数失败", "user_id", u.ID, "err", derr)
			personal = n
			cnt = 0
		}
		if personal >= sysuser.FailThreshold {
			if lerr := s.users.MarkLocked(ctx, u.ID, time.Now().Add(sysuser.LockDuration)); lerr != nil {
				slog.Warn("DB 写锁定失败", "user_id", u.ID, "err", lerr)
			}
			if rerr2 := s.lock.MarkLocked(ctx, u.ID, sysuser.LockDuration); rerr2 != nil {
				slog.Warn("Redis 写锁定标记失败（DB locked_until 已生效）", "user_id", u.ID, "err", rerr2)
			}
		}
	}

	// IP 撞库计数恒写入（探测“账号是否存在”同样计入）
	if rerr := s.guard.RecordIPFail(ctx, p.IP); rerr != nil {
		slog.Warn("IP 撞库计数失败", "ip", p.IP, "err", rerr)
	}

	s.recordLog(ctx, uidOf(u), idMask, t, via, false, reason, p)
	return authn.ErrBadCredential
}

// success 成功路径：签发会话 + 更新最近登录 + 清计数 + 成功日志。
func (s *AuthService) success(ctx context.Context, u *sysuser.User, t loginlog.Type, via sysuser.Via, idMask string, p RequestParams, now time.Time) (*LoginResp, error) {
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

	s.recordLog(ctx, u.ID, idMask, t, via, true, "", p)

	u2 := toUserView(u)
	return &LoginResp{
		Token:         tok.Value,
		ExpiresIn:     int64(tok.TTL / time.Second),
		User:          u2,
		PasswordStale: u.PasswordStale(now),
	}, nil
}

// recordLog 登录日志：**写事务外**（独立 goroutine + 独立 ctx），
// 失败只告警不影响登录结果（docs §3.2 规则 1、§10）。
func (s *AuthService) recordLog(ctx context.Context, userID int64, identMask string, t loginlog.Type, via sysuser.Via, ok bool, reason loginlog.FailReason, p RequestParams) {
	go func(p RequestParams) {
		defer func() {
			// 日志写失败/异常绝不能拖垮登录主流程（docs §3.2 规则 1）
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

// randDigits 短信验证码（6 位数字，crypto/rand；docs §5）。
func randDigits(n int) string {
	digits := "0123456789"
	out := make([]byte, n)
	for i := range out {
		idx, _ := crand.Int(crand.Reader, big.NewInt(int64(len(digits))))
		out[i] = digits[idx.Int64()]
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
