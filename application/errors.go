package application

import (
	"errors"

	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/sysuser"
	"dugu-hr-saas-admin/pkg/code"
)

// ErrBadRequest 业务级参数/格式错误（api 层不需要额外细分）。
var (
	ErrIncorrectRange = errors.New("application: bad time range")
)

// CodeOf 领域错误 → pkg/code 业务码。接口层组装响应时使用。
// 途径：typed As（锁定剩秒）→ domain 哨兵 → 兜底（未知错误视为数据库错误，
// 不把内幕漏给用户，docs §8.4 延伸）。
func CodeOf(err error) int {
	if err == nil {
		return code.SUCCESS
	}

	var le *authn.ErrLocked
	if errors.As(err, &le) {
		return code.ErrorUserLocked
	}

	switch {
	case errors.Is(err, sysuser.ErrNotFound):
		return code.ErrorSysUserNotFound
	case errors.Is(err, sysuser.ErrUsernameTaken):
		return code.ErrorUserNameTaken
	case errors.Is(err, sysuser.ErrMobileTaken):
		return code.ErrorMobileTaken
	case errors.Is(err, sysuser.ErrBadUsername), errors.Is(err, sysuser.ErrBadMobile):
		return code.ErrorBadIdentifierFormat
	case errors.Is(err, sysuser.ErrWeakPassword):
		return code.ErrorWeakPassword
	case errors.Is(err, authn.ErrBadCredential):
		return code.ErrorBadCredential
	case errors.Is(err, authn.ErrCaptchaRequired):
		return code.ErrorCaptchaRequired
	case errors.Is(err, authn.ErrCaptchaFailed):
		return code.ErrorCaptchaFailed
	case errors.Is(err, authn.ErrSmsCodeError):
		return code.ErrorSmsCodeError
	case errors.Is(err, authn.ErrSmsExpired):
		return code.ErrorSmsExpired
	case errors.Is(err, authn.ErrSmsSendFailed):
		return code.ErrorSendSms
	case errors.Is(err, authn.ErrRateLimited):
		return code.ErrorRateLimited
	case errors.Is(err, ErrIncorrectRange):
		return code.InvalidParams
	case IsSuperAdminProtected(err):
		return code.ErrorSuperAdminProtected
	}
	return code.ErrorDatabase
}
