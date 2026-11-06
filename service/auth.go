// Package service 业务层：编排 DAO/cache/jwt，实现登录管线与账号管理。
// 登录管线三条铁律（docs §4.1）：
//  1. 三个入口（账号密码/手机密码/手机短信）解析之后汇合同一段管线；
//  2. 状态判定（停用/锁定）在因子校验之后——不给无凭证者确认账号状态的机会；
//  3. 失败计数按 user_id 计，不按标识分桶。
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/consts"
	"dugu-hr-saas-admin/pkg/utils/captcha"
	"dugu-hr-saas-admin/pkg/utils/encryption"
	"dugu-hr-saas-admin/pkg/utils/jwt"
	"dugu-hr-saas-admin/pkg/utils/math"
	"dugu-hr-saas-admin/pkg/utils/uid"
	"dugu-hr-saas-admin/repository/cache"
	"dugu-hr-saas-admin/repository/db/dao"
	"dugu-hr-saas-admin/repository/db/model"
	"dugu-hr-saas-admin/types"
)

// tokenService 会话签发器：惰性初始化（配置在 main 里 loading() 之后可用；
// 首个业务请求发生在 InitConfig 之后，进程内无竞态）。
var (
	tokenOnce sync.Once
	tokenSrc  *jwt.Token
)

func tokenService() *jwt.Token {
	tokenOnce.Do(func() {
		tokenSrc = jwt.New(conf.Config.EncryptSecret.JwtSecret)
	})
	return tokenSrc
}

// AuthSrv 认证业务。
type AuthSrv struct{}

// GetAuthSrv 单例（gin-mall 风格）。
func GetAuthSrv() *AuthSrv { return &AuthSrv{} }

// SmsSender 短信发送接口。接入真实通道（阿里云/腾讯云）时新增实现即可。
type SmsSender interface {
	Send(ctx context.Context, mobilePlain, code string) error
}

// logSmsSender 开发/联调用桩实现：不外发，仅记录（真实资费与合规审批另接 provider）。
type logSmsSender struct{}

func (logSmsSender) Send(_ context.Context, mobilePlain, code string) error {
	slog.Info("[sms-stub] 发送验证码（生产环境请替换为真实通道实现）",
		"mobile", encryption.MaskMobile(mobilePlain), "code", code)
	return nil
}

var smsSender SmsSender = logSmsSender{}

// ---------- 图形验证码 ----------

// GetCaptcha 获取图形验证码（5 位数字，一次性，5 分钟有效）。
func (s *AuthSrv) GetCaptcha() (*types.CaptchaGetResp, error) {
	id, answer, image, err := captcha.Generate()
	if err != nil {
		return nil, fmt.Errorf("gen captcha: %w", err)
	}
	if err := cache.SaveCaptcha(id, answer); err != nil {
		return nil, fmt.Errorf("save captcha: %w", err)
	}
	return &types.CaptchaGetResp{CaptchaId: id, Image: image}, nil
}

// ---------- 登录 ----------

// LoginPassword 账号/手机 + 密码登录（docs §4 管线 PASSWORD 入口）。
func (s *AuthSrv) LoginPassword(ctx context.Context, req *types.LoginPasswordReq, ip, ua string) (*types.LoginResp, error) {
	ident := strings.TrimSpace(req.Identifier)

	// IP 限流（20 次/分钟）
	if !limitPass(cache.IPLoginAllowed, ip) {
		return nil, s.rejectNew(ctx, ip, ua, consts.LoginTypePassword, ident, consts.FailReasonRateLimited)
	}
	// IP 撞库兜底（50 次/小时）
	if !limitPass(cache.IPFailAllowed, ip) {
		return nil, s.rejectNew(ctx, ip, ua, consts.LoginTypePassword, ident, consts.FailReasonRateLimited)
	}

	// ① 解析 user（只判存在，不下状态结论）
	u, via, err := s.resolveUser(ctx, ident)
	if err != nil {
		return nil, err
	}
	idMask := encryption.MaskInput(ident)

	// ② 图形码校验（连续失败 ≥3 后必须，docs §8.7）
	if nerr := s.guardCaptcha(ctx, u, ident, ip, req.CaptchaId, req.CaptchaCode); nerr != nil {
		return nil, nerr
	}

	// ③ 因子校验（密码）；不存在账号陪跑假 hash（docs §8.1）
	if !s.checkPassword(u, req.Password) {
		return nil, s.failAttempt(ctx, u, ip, ua, consts.LoginTypePassword, via, idMask, consts.FailReasonBadCredential)
	}

	// ④ 状态判定（在因子校验之后；停用不计失败次数，docs §6/§10）
	if u.IsDisabled() {
		s.writeLoginLog(u.Id, idMask, consts.LoginTypePassword, via, false, consts.FailReasonDisabled, ip, ua)
		return nil, ErrBadCredential // 对外统一文案：账号或密码错误
	}
	if rem, locked := s.lockState(ctx, u); locked {
		s.writeLoginLog(u.Id, idMask, consts.LoginTypePassword, via, false, consts.FailReasonLocked, ip, ua)
		return nil, Locked(rem)
	}

	// ⑤ 签发会话 + 写日志 + 清计数
	return s.finishLogin(ctx, u, consts.LoginTypePassword, via, idMask, ip, ua)
}

// LoginSms 手机 + 短信码登录（SMS 入口；验证码通过后汇入状态管线）。
func (s *AuthSrv) LoginSms(ctx context.Context, req *types.LoginSmsReq, ip, ua string) (*types.LoginResp, error) {
	norm, err := encryption.NormalizeMobile(req.Mobile)
	if err != nil {
		return nil, ErrBadIdentifierFormat
	}
	if !limitPass(cache.IPLoginAllowed, ip) {
		return nil, s.rejectNew(ctx, ip, ua, consts.LoginTypeSMS, norm, consts.FailReasonRateLimited)
	}

	// 归一化的 11 位串不会命中 username（创建期已禁止 11 位纯数字），只会走 mobile_hash
	mHash := encryption.MobileHMAC(norm, conf.Config.EncryptSecret.MobileHmac)
	u, err := dao.NewSysUserDao(ctx).GetByMobileHash(mHash)
	if err != nil {
		return nil, err
	}
	idMask := encryption.MaskMobile(norm)
	via := consts.IdentViaMobile

	// 因子：验证码校验（用途隔离在 key 内）
	res, verr := cache.VerifySmsCode(consts.SmsPurposeLogin, mHash, req.Code)
	if verr != nil {
		return nil, fmt.Errorf("verify sms code: %w", verr)
	}
	switch res {
	case smsOK:
		// 继续状态管线
	case smsNotFound:
		// 一个口径：无存在性信号——未注册/未发送/过期统一 SMS_EXPIRED
		// （u==nil 时也如此，杜绝“按返回码探测哪些手机号是管理员”）
		if u == nil {
			s.writeLoginLog(0, idMask, consts.LoginTypeSMS, via, false, consts.FailReasonSmsExpired, ip, ua)
			return nil, ErrSmsExpired
		}
		s.writeLoginLog(u.Id, idMask, consts.LoginTypeSMS, via, false, consts.FailReasonSmsExpired, ip, ua)
		return nil, ErrSmsExpired
	case smsBad:
		if u == nil {
			s.writeLoginLog(0, idMask, consts.LoginTypeSMS, via, false, consts.FailReasonSmsCodeError, ip, ua)
			return nil, ErrSmsCodeError
		}
		return nil, s.failAttempt(ctx, u, ip, ua, consts.LoginTypeSMS, via, idMask, consts.FailReasonSmsCodeError)
	case smsExhausted:
		if u == nil {
			s.writeLoginLog(0, idMask, consts.LoginTypeSMS, via, false, consts.FailReasonSmsExpired, ip, ua)
			return nil, ErrSmsExpired
		}
		return nil, s.failAttempt(ctx, u, ip, ua, consts.LoginTypeSMS, via, idMask, consts.FailReasonSmsExpired)
	}

	if u == nil {
		return nil, ErrBadCredential
	}

	// 状态判定（同密码管线）
	if u.IsDisabled() {
		s.writeLoginLog(u.Id, idMask, consts.LoginTypeSMS, via, false, consts.FailReasonDisabled, ip, ua)
		return nil, ErrBadCredential
	}
	if rem, locked := s.lockState(ctx, u); locked {
		s.writeLoginLog(u.Id, idMask, consts.LoginTypeSMS, via, false, consts.FailReasonLocked, ip, ua)
		return nil, Locked(rem)
	}

	return s.finishLogin(ctx, u, consts.LoginTypeSMS, via, idMask, ip, ua)
}

// SendSms 发送登录短信（docs §5 全部规则）。
func (s *AuthSrv) SendSms(ctx context.Context, req *types.SendSmsReq, ip string) error {
	// 前置图形码（短信轰炸是真实资损）
	ok, err := cache.VerifyCaptcha(req.CaptchaId, req.CaptchaCode)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCaptchaFailed
	}

	norm, err := encryption.NormalizeMobile(req.Mobile)
	if err != nil {
		return ErrBadIdentifierFormat
	}
	mHash := encryption.MobileHMAC(norm, conf.Config.EncryptSecret.MobileHmac)

	// 配额（Redis 故障 → 配额无法判定，短信功能保守拒绝）
	if ok, err := cache.SmsMinuteAllowed(consts.SmsPurposeLogin, mHash); err != nil {
		return fmt.Errorf("sms minute limit: %w", err)
	} else if !ok {
		return ErrRateLimited
	}
	if ok, err := cache.SmsDailyAllowed(consts.SmsPurposeLogin, mHash); err != nil {
		return fmt.Errorf("sms daily limit: %w", err)
	} else if !ok {
		return ErrRateLimited
	}
	if ok, err := cache.SmsIPDailyAllowed(ip); err != nil {
		return fmt.Errorf("sms ip daily limit: %w", err)
	} else if !ok {
		return ErrRateLimited
	}

	// 防枚举（docs §5）：查到人才真正发；查不到 → 返回“已发送”，日志记 SEND_SKIPPED
	u, err := dao.NewSysUserDao(ctx).GetByMobileHash(mHash)
	if err != nil {
		return err
	}
	if u == nil {
		s.writeLoginLog(0, encryption.MaskMobile(norm), consts.LoginTypeSMS, consts.IdentViaMobile, false, consts.FailReasonSendSkipped, ip, "")
		return nil
	}

	plain, err := encryption.MobileDecrypt(u.MobileEnc, conf.Config.EncryptSecret.MobileAes)
	if err != nil {
		return fmt.Errorf("decrypt mobile: %w", err)
	}
	code := math.RandDigits(consts.SmsLength)
	if err := cache.SaveSmsCode(consts.SmsPurposeLogin, mHash, code); err != nil {
		return fmt.Errorf("save sms code: %w", err)
	}
	if err := smsSender.Send(ctx, plain, code); err != nil {
		return fmt.Errorf("send sms: %w", err)
	}
	return nil
}

// ---------- 登出 / 个人信息 ----------

// Logout 登出当前会话（删 sess:{jti} 与硬上限键）。
func (s *AuthSrv) Logout(uidV int64, jti string) error {
	return cache.DeleteSession(uidV, jti)
}

// Me 当前用户信息（uid 来自鉴权中间件）。
func (s *AuthSrv) Me(ctx context.Context, uidV int64) (*types.UserView, bool, error) {
	u, err := dao.NewSysUserDao(ctx).GetByID(uidV)
	if err != nil {
		return nil, false, err
	}
	if u == nil {
		return nil, false, ErrBadCredential
	}
	return toUserView(u), passwordStale(u), nil
}

// ---------- 管线内部 ----------

// resolveUser 登录标识解析（docs §3.3③，两条规则不会同时命中）：
//
//	① username 精确匹配（唯一索引）
//	② 形似手机号 → 归一化 → mobile_hash 匹配
//	③ 都未命中 → nil
func (s *AuthSrv) resolveUser(ctx context.Context, ident string) (*model.SysUser, string, error) {
	ud := dao.NewSysUserDao(ctx)

	u, err := ud.GetByUsername(ident) // ①
	if err != nil {
		return nil, "", err
	}
	if u != nil {
		return u, consts.IdentViaUsername, nil
	}

	if encryption.LooksLikeMobile(ident) { // ②
		norm, nerr := encryption.NormalizeMobile(ident)
		if nerr == nil {
			u2, err2 := ud.GetByMobileHash(encryption.MobileHMAC(norm, conf.Config.EncryptSecret.MobileHmac))
			if err2 != nil {
				return nil, "", err2
			}
			if u2 != nil {
				return u2, consts.IdentViaMobile, nil
			}
		}
	}
	return nil, "", nil
}

// checkPassword 因子校验：bcrypt 主路；sha256 旧算法透明升级（docs §3.3②）；
// 账号不存在陪跑假 hash 抹平时序差（docs §8.1）。
func (s *AuthSrv) checkPassword(u *model.SysUser, pwd string) bool {
	if u == nil {
		encryption.CheckBcrypt(encryption.DecoyHash, pwd) // 陪跑，结果忽略
		return false
	}
	if u.PasswordHash == "" {
		return false // 无密码（仅短信登录）
	}

	switch u.PasswordAlgo {
	case "", encryption.AlgoBcrypt:
		return encryption.CheckBcrypt(u.PasswordHash, pwd)
	case encryption.AlgoSHA256:
		if encryption.LegacySHA256(u.Salt, pwd) == u.PasswordHash {
			s.upgradeLegacyPassword(u, pwd) // 校验成功 → 透明升级 bcrypt 并清 salt
			return true
		}
		return false
	default:
		slog.Warn("未知 password_algo，按 bcrypt 校验", "algo", u.PasswordAlgo)
		return encryption.CheckBcrypt(u.PasswordHash, pwd)
	}
}

// upgradeLegacyPassword 旧算法迁移：bcrypt 覆写 + salt 清空（docs §3.3②）。
func (s *AuthSrv) upgradeLegacyPassword(u *model.SysUser, pwd string) {
	hash, err := encryption.HashPassword(pwd)
	if err != nil {
		slog.Warn("旧密码透明升级失败", "user_id", u.Id, "err", err)
		return
	}
	bg, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := dao.NewSysUserDao(bg).UpdatePassword(u.Id, hash, encryption.AlgoBcrypt, "", time.Now()); err != nil {
		slog.Warn("旧密码透明升级失败", "user_id", u.Id, "err", err)
		return
	}
	slog.Info("旧算法密码已升级为 bcrypt", "user_id", u.Id)
}

// guardCaptcha 图形码守卫（docs §8.7：密码登录连续失败 3 次后必带）。
// 返回 nil 表示放行；不满足时先按 CAPTCHA_FAILED 计数与记账。
func (s *AuthSrv) guardCaptcha(ctx context.Context, u *model.SysUser, ident, ip, captID, captCode string) error {
	if u == nil {
		return nil // 不存在账号交给统一 BAD_CREDENTIAL 口径
	}
	streak, err := cache.StreakUID(u.Id)
	if err != nil {
		slog.Warn("读取连续失败步数失败（保守放行，DB 锁定仍兜底）", "user_id", u.Id, "err", err)
		return nil
	}
	if streak < consts.CaptchaStreakTrigger {
		return nil
	}
	if captID == "" || captCode == "" {
		return ErrCaptchaRequired
	}
	ok, err := cache.VerifyCaptcha(captID, captCode)
	if err != nil {
		return fmt.Errorf("verify captcha: %w", err)
	}
	if !ok {
		_ = s.failAttempt(ctx, u, ip, "", consts.LoginTypePassword, "",
			encryption.MaskInput(ident), consts.FailReasonCaptchaFailed)
		return ErrCaptchaFailed
	}
	return nil
}

// rejectNew “账号不存在/限流拒绝”侧：写日志（user_id=0）并统一返回 BAD_CREDENTIAL（限流除外）。
func (s *AuthSrv) rejectNew(ctx context.Context, ip, ua, loginType, ident, reason string) error {
	idMask := ""
	if ident != "" {
		idMask = encryption.MaskInput(ident)
	}
	s.writeLoginLog(0, idMask, loginType, "", false, reason, ip, ua)
	if reason == consts.FailReasonRateLimited {
		return ErrRateLimited
	}
	return ErrBadCredential
}

// failAttempt 失败计数 + 锁定 + 日志（docs §6：user_id 主维度 + IP 兜底）。
// 不存在账号（u==nil）按标识在 Redis 兜底计数（docs §10）。
func (s *AuthSrv) failAttempt(ctx context.Context, u *model.SysUser, ip, ua, loginType, via, idMask, reason string) error {
	if u != nil {
		// Redis 计数（加速层；出错不中断，DB 双写兜底）
		n, rerr := cache.IncrFail(u.Id)
		if rerr != nil {
			slog.Warn("Redis 失败计数失败（DB 兜底仍会执行）", "user_id", u.Id, "err", rerr)
			n = 0
		}
		// 连续失败步数（触发图形码）
		if _, serr := cache.StreakIncrUID(u.Id); serr != nil {
			slog.Warn("连续失败步数写入失败", "user_id", u.Id, "err", serr)
		}

		// DB 双写（Redis 是加速，DB 是兜底）
		cnt, triggerLock, derr := dao.NewSysUserDao(ctx).IncFail(u.Id, consts.FailCountThreshold, consts.LockDuration, time.Now())
		if derr != nil {
			slog.Warn("DB 失败计数失败", "user_id", u.Id, "err", derr)
		}
		personalCount := cnt
		if cnt == 0 && derr != nil {
			personalCount = n // DB 不可用时以 Redis 结果为准
		}
		if personalCount >= consts.FailCountThreshold || triggerLock {
			if cerr := cache.SetFailLock(u.Id); cerr != nil {
				slog.Warn("写 Redis 锁定标记失败（DB locked_until 已生效）", "user_id", u.Id, "err", cerr)
			}
		}
	} else {
		// 不存在账号：按标识兜底计数（key 维度：脱敏标识+IP，防按标识爆破）
		if _, err := cache.StreakKeyIncr(idMask, ip); err != nil {
			slog.Warn("不存在账号兜底计数失败", "err", err)
		}
	}

	// IP 撞库计数恒写入（u==nil 的探测同样计入）
	if err := cache.IPFailIncr(ip); err != nil {
		slog.Warn("IP 撞库计数失败", "ip", ip, "err", err)
	}

	s.writeLoginLog(uidOf(u), idMask, loginType, via, false, reason, ip, ua)
	return ErrBadCredential
}

// lockState 锁定判定：Redis 加速，DB 兜底；绝不因 Redis 挂掉放行（docs §6 反例条款）。
func (s *AuthSrv) lockState(ctx context.Context, u *model.SysUser) (remaining int64, locked bool) {
	rem, err := cache.LockRemainingSecs(u.Id)
	if err == nil {
		return rem, rem > 0
	}
	slog.Warn("Redis 锁定查询失败，回落 DB 判定", "user_id", u.Id, "err", err)

	failCount, lockedUntil, derr := dao.NewSysUserDao(ctx).GetLoginState(u.Id)
	_ = failCount
	if derr != nil {
		slog.Warn("DB 锁定查询也失败，按状态列保守判定", "user_id", u.Id, "err", derr)
		if u.Status == model.UserStatusLocked {
			return int64(consts.LockDuration / time.Second), true
		}
		return 0, false
	}
	if lockedUntil != nil && lockedUntil.After(time.Now()) {
		return int64(time.Until(*lockedUntil) / time.Second), true
	}
	return 0, false
}

// finishLogin 成功路径：签发会话 + 写日志 + 清计数 + 更新最近登录。
func (s *AuthSrv) finishLogin(ctx context.Context, u *model.SysUser, loginType, via, idMask, ip, ua string) (*types.LoginResp, error) {
	token, jti, err := tokenService().GenerateToken(u.Id)
	if err != nil {
		return nil, fmt.Errorf("gen token: %w", err)
	}
	if err := cache.CreateSession(u.Id, jti); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	now := time.Now()
	if err := dao.NewSysUserDao(ctx).UpdateLoginState(u.Id, ip, now); err != nil {
		slog.Warn("更新最近登录失败", "user_id", u.Id, "err", err)
	}
	if err := cache.ClearFail(u.Id); err != nil {
		slog.Warn("清除失败计数失败", "user_id", u.Id, "err", err)
	}
	if err := cache.ClearFailLock(u.Id); err != nil {
		slog.Warn("清除锁定标记失败", "user_id", u.Id, "err", err)
	}
	_ = cache.StreakReset(u.Id, idMask, ip)

	s.writeLoginLog(u.Id, idMask, loginType, via, true, "", ip, ua)

	return &types.LoginResp{
		Token:         token,
		ExpiresIn:     int64(consts.IdleSessionTTL / time.Second),
		User:          *toUserView(u),
		PasswordStale: passwordStale(u),
	}, nil
}

// writeLoginLog 登录日志：写事务外（独立 goroutine + 独立 ctx），
// 失败只告警不影响登录结果（docs §3.2 规则 1、§10）。
func (s *AuthSrv) writeLoginLog(userID int64, identMask, loginType, via string, success bool, failReason, ip, ua string) {
	go func() {
		defer func() {
			// 日志写失败/异常绝不能拖垮登录主流程（docs §3.2 规则 1）
			if r := recover(); r != nil {
				slog.Warn("登录日志异步写入 panic（已恢复，需告警）", "panic", r)
			}
		}()
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if len(ua) > 512 {
			ua = ua[:512]
		}
		err := dao.NewSysLoginLogDao(bg).Create(&model.SysLoginLog{
			Id:             uid.New(),
			UserId:         userID,
			IdentifierMask: identMask,
			LoginType:      loginType,
			IdentVia:       via,
			Success:        success,
			FailReason:     failReason,
			Ip:             ip,
			Ua:             ua,
		})
		if err != nil {
			slog.Warn("登录日志写库失败（需监控告警）", "err", err)
		}
	}()
}

// toUserView PO → 展示视图（手机只见 mask）。
func toUserView(u *model.SysUser) *types.UserView {
	v := &types.UserView{
		Id:       u.Id,
		Username: u.Username,
		Name:     u.Name,
		Email:    u.Email,
		Mobile:   u.MobileMask,
		Status:   u.Status,
	}
	if !u.CreatedAt.IsZero() {
		v.CreatedAt = u.CreatedAt.Format(time.DateTime)
	}
	return v
}

// passwordStale 密码超 180 天提示改密（不强制，docs §8.2）。
func passwordStale(u *model.SysUser) bool {
	return u.PwdUpdatedAt != nil && time.Since(*u.PwdUpdatedAt) > time.Duration(consts.PasswordStaleDays)*24*time.Hour
}

// uidOf user 为 nil 时返回 0。
func uidOf(u *model.SysUser) int64 {
	if u == nil {
		return 0
	}
	return u.Id
}

// limitPass 限流探测封装：探测出错时保守放行并告警（失败锁定有 DB 兜底）。
func limitPass(probe func(ip string) (bool, error), ip string) bool {
	ok, err := probe(ip)
	if err != nil {
		slog.Warn("限流探测失败，按放行处理", "ip", ip, "err", err)
		return true
	}
	return ok
}

// sms 校验结果（取值与 cache.VerifySmsCode 的 smsVerifyResult 对齐）
const (
	smsOK        = 0
	smsBad       = 1
	smsExhausted = 2
	smsNotFound  = 3
)
