package code

// msgMap 业务码 → 面向用户的文案（zh-CN）。
// 安全要求（docs §8.4）：对外只有一句“账号或密码错误”，
// 不区分账号不存在 / 密码错 / 已停用——ErrorBadCredential 与 ErrorUserDisabled 在
// 响应层共用 BAD_CREDENTIAL 的文案，仅 status code 不同（前端按 code 处理锁定/停用跳转）。
var msgMap = map[int]string{
	SUCCESS:                  "操作成功",
	UpdateSuccess:            "操作成功",
	ERROR:                    "服务器错误，请稍后重试",
	InvalidParams:            "参数错误",
	ErrorAuthCheckTokenFail:  "登录已失效，请重新登录",
	ErrorAuthRequired:        "请先登录",
	ErrorBadCredential:       "账号或密码错误",
	ErrorUserLocked:          "账号或密码错误", // 统一文案（剩余秒数放 data）
	ErrorUserDisabled:        "账号或密码错误", // 统一文案
	ErrorCaptchaFailed:       "图形验证码错误或已过期，请重试",
	ErrorCaptchaRequired:     "请先完成图形验证码",
	ErrorSmsCodeError:        "验证码错误",
	ErrorSmsExpired:          "验证码已过期，请重新获取",
	ErrorRateLimited:         "请求过于频繁，请稍后再试",
	ErrorSendSms:             "短信发送失败，请稍后再试",
	ErrorUserNameTaken:       "账号已存在",
	ErrorMobileTaken:         "手机号已绑定其他账号",
	ErrorSysUserNotFound:     "账号不存在",
	ErrorWeakPassword:        "密码须不少于 10 位且至少含两类字符",
	ErrorBadIdentifierFormat: "账号或手机号格式不合法",
	ErrorSuperAdminProtected: "内置超管账号受保护，禁止该操作",
	ErrorDatabase:            "数据库操作出错，请重试",
}

// GetMsg 获取业务码对应文案，未知码回退通用 ERROR 文案。
func GetMsg(code int) string {
	if m, ok := msgMap[code]; ok {
		return m
	}
	return msgMap[ERROR]
}
