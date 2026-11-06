package dao

import "errors"

// dao 层到 service 层的错误哨兵：service 统一映射为 pkg/e 业务码。
var (
	ErrNotFound      = errors.New("记录不存在")
	ErrUserNameTaken = errors.New("账号已存在")
	ErrMobileTaken   = errors.New("手机号已绑定其他账号")
)
