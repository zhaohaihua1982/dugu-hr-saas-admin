// Package application —— 用例容器（bootstrap 装配后整体交给接口层）。
package application

// Services 应用层用例集合。
type Services struct {
	Auth  *AuthService
	Users *SysUserService
	Logs  *LoginLogService
}
