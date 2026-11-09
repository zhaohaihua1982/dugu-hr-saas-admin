// Package application —— 应用层根包：用例容器与总错误映射。
// （用例本体按领域拆分子包：auth / sysuser / loginlog；共享上下文类型见 types）
package application

import (
	"errors"

	"dugu-hr-saas-admin/application/auth"
	apploginlog "dugu-hr-saas-admin/application/loginlog"
	appmenus "dugu-hr-saas-admin/application/menus"
	appsysuser "dugu-hr-saas-admin/application/sysuser"
	"dugu-hr-saas-admin/domain/authn"
	dmenu "dugu-hr-saas-admin/domain/menu"
	"dugu-hr-saas-admin/domain/sysuser"
	"dugu-hr-saas-admin/pkg/code"
)

// Services 应用层用例集合（bootstrap 装配后整体交给 interfaces）。
type Services struct {
	Auth  *auth.Service
	Users *appsysuser.Service
	Logs  *apploginlog.Service
	Menus *appmenus.Service
}

// CodeOf 领域错误 → pkg/code 业务码（api 层组装响应时使用）。
// 途径：typed As（锁定剩秒）→ domain 哨兵 → 用例哨兵 → 兜底
// （未知错误视为数据库错误，不把内幕漏给用户，docs §8.4 延伸）。
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
	case errors.Is(err, authn.ErrBadCodeFormat):
		return code.InvalidParams
	case errors.Is(err, apploginlog.ErrBadRange):
		return code.InvalidParams
	case errors.Is(err, appsysuser.ErrSuperAdminProtected):
		return code.ErrorSuperAdminProtected
	case errors.Is(err, appsysuser.ErrSelfOperation):
		return code.ErrorSelfOperation
	case errors.Is(err, appmenus.ErrBadMenuID), errors.Is(err, dmenu.ErrNotFound):
		return code.ErrorMenuNotFound
	case errors.Is(err, dmenu.ErrNotFound):
		return code.ErrorMenuNotFound
	case errors.Is(err, dmenu.ErrHasChildren):
		return code.ErrorMenuHasChildren
	case errors.Is(err, dmenu.ErrActionCodeTaken):
		return code.ErrorMenuActionCodeTaken
	case errors.Is(err, dmenu.ErrLangUnsupported):
		return code.ErrorMenuLangUnsupported
	case errors.Is(err, dmenu.ErrNameRequired):
		return code.ErrorMenuNameRequired
	case errors.Is(err, dmenu.ErrBadParams):
		return code.ErrorMenuBadParams
	}
	return code.ErrorDatabase
}
