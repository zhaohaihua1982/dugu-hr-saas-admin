package service

import (
	"errors"

	"github.com/redis/go-redis/v9"

	"dugu-hr-saas-admin/pkg/e"
	"dugu-hr-saas-admin/repository/db/dao"
)

// ---------- 业务哨兵错误（service 统一映射为 pkg/e 业务码） ----------

var (
	ErrBadIdentifierFormat = errors.New("账号/手机号格式不合法")
	ErrWeakPassword        = errors.New("密码不满足策略")
	ErrBadCredential       = errors.New("账号或密码错误")
	ErrUserDisabled        = errors.New("账号已停用")
	ErrUserLocked          = &lockedError{} // 锁定错误，携带剩余秒数
	ErrCaptchaRequired     = errors.New("需要图形验证码")
	ErrCaptchaFailed       = errors.New("图形验证码错误")
	ErrSmsCodeError        = errors.New("短信验证码错误")
	ErrSmsExpired          = errors.New("短信验证码过期或已用")
	ErrSendSms             = errors.New("短信发送失败")
	ErrRateLimited         = errors.New("请求过于频繁")
	ErrSuperAdminProtected = errors.New("超管账号受保护")
	ErrForbidden           = errors.New("无权操作该账号")
	ErrInternal            = errors.New("服务内部错误")
)

// lockedError 锁定错误： remaining_seconds 放响应 data（docs §10）。
type lockedError struct {
	Remaining int64 // 秒
}

func (e *lockedError) Error() string        { return "账号锁定中" }
func (e *lockedError) Is(target error) bool { return target == ErrUserLocked }

// Locked 组装带剩余秒数的锁定错误。
func Locked(remainingSecs int64) error {
	return &lockedError{Remaining: remainingSecs}
}

// IsLocked 判定 err 是否锁定错误并取剩余秒数。
func IsLocked(err error) (bool, int64) {
	var le *lockedError
	if errors.As(err, &le) {
		return true, le.Remaining
	}
	return false, 0
}

// CodeOf 错误 → pkg/e 业务码。api 层组装响应使用。
// 顺序要求：typed As（锁定剩秒）→ domains → 兜底。
func CodeOf(err error) int {
	if err == nil {
		return e.SUCCESS
	}

	var le *lockedError
	if errors.As(err, &le) {
		return e.ErrorUserLocked
	}

	// dao 层哨兵
	switch {
	case errors.Is(err, dao.ErrNotFound):
		return e.ErrorSysUserNotFound
	case errors.Is(err, dao.ErrUserNameTaken):
		return e.ErrorUserNameTaken
	case errors.Is(err, dao.ErrMobileTaken):
		return e.ErrorMobileTaken
	}

	// service 层哨兵
	switch {
	case errors.Is(err, ErrBadCredential):
		return e.ErrorBadCredential
	case errors.Is(err, ErrUserDisabled):
		return e.ErrorUserDisabled
	case errors.Is(err, ErrCaptchaRequired):
		return e.ErrorCaptchaRequired
	case errors.Is(err, ErrCaptchaFailed):
		return e.ErrorCaptchaFailed
	case errors.Is(err, ErrSmsCodeError):
		return e.ErrorSmsCodeError
	case errors.Is(err, ErrSmsExpired):
		return e.ErrorSmsExpired
	case errors.Is(err, ErrSendSms):
		return e.ErrorSendSms
	case errors.Is(err, ErrRateLimited):
		return e.ErrorRateLimited
	case errors.Is(err, ErrWeakPassword):
		return e.ErrorWeakPassword
	case errors.Is(err, ErrBadIdentifierFormat):
		return e.ErrorBadIdentifierFormat
	case errors.Is(err, ErrSuperAdminProtected):
		return e.ErrorSuperAdminProtected
	case errors.Is(err, ErrForbidden):
		return e.ErrorSuperAdminProtected
	case errors.Is(err, ErrIncorrectRange):
		return e.InvalidParams
	}

	// 环境故障类（存储层/Redis）
	if errors.Is(err, redis.ErrClosed) {
		return e.ErrorDatabase
	}
	if isStorageError(err) {
		return e.ErrorDatabase
	}

	return e.ERROR
}

// isStorageError 包装的存储层错误兜底（dao 的 fmt.Errorf 不接哨兵，→ 归为数据库错误）。
// 这里用**保守策略**：未识别的 service/db 错误统一按数据错误提示，
// 避免把 SQL 细节漏给前端（docs：安全清单 4 的延伸）。
func isStorageError(err error) bool {
	// 未知错误一律视为存储/系统错误，由 e.CodeOf 兜底返回 ErrorDatabase
	return true
}
