package code

// msgMap 业务码 → 面向用户的文案（zh-CN）。
// 安全要求（docs §8.4）：对外只有一句“账号或密码错误”，
// 不区分账号不存在 / 密码错 / 已停用——ErrorBadCredential 与 ErrorUserDisabled 在
// 响应层共用 BAD_CREDENTIAL 的文案，仅 status code 不同（前端按 code 处理锁定/停用跳转）。
var msgMap = map[int]string{
	SUCCESS:                         "操作成功",
	UpdateSuccess:                   "操作成功",
	ERROR:                           "服务器错误，请稍后重试",
	InvalidParams:                   "参数错误",
	ErrorAuthCheckTokenFail:         "登录已失效，请重新登录",
	ErrorAuthRequired:               "请先登录",
	ErrorBadCredential:              "账号或密码错误",
	ErrorUserLocked:                 "账号或密码错误", // 统一文案（剩余秒数放 data）
	ErrorUserDisabled:               "账号或密码错误", // 统一文案
	ErrorCaptchaFailed:              "图形验证码错误或已过期，请重试",
	ErrorCaptchaRequired:            "请先完成图形验证码",
	ErrorSmsCodeError:               "验证码错误",
	ErrorSmsExpired:                 "验证码已过期，请重新获取",
	ErrorRateLimited:                "请求过于频繁，请稍后再试",
	ErrorSendSms:                    "短信发送失败，请稍后再试",
	ErrorUserNameTaken:              "账号已存在",
	ErrorMobileTaken:                "手机号已绑定其他账号",
	ErrorSysUserNotFound:            "账号不存在",
	ErrorWeakPassword:               "密码须不少于 10 位且至少含两类字符",
	ErrorBadIdentifierFormat:        "账号或手机号格式不合法",
	ErrorSuperAdminProtected:        "内置超管账号受保护，禁止该操作",
	ErrorDatabase:                   "数据库操作出错，请重试",
	ErrorMenuNotFound:               "菜单不存在",
	ErrorMenuHasChildren:            "该菜单存在子节点，请先处理子节点",
	ErrorMenuActionCodeTaken:        "动作编码(action_code)已被占用",
	ErrorMenuBadParams:              "菜单参数不合法",
	ErrorMenuLangUnsupported:        "语言不被支持（仅支持已注册语言）",
	ErrorMenuNameRequired:           "菜单名称（默认语言译文）不能为空",
	ErrorMenuSameLevelTaken:         "名称或请求地址与其同级重复，或该类型下权限点(code)重复",
	ErrorI18nTarget:                 "翻译资源类型或字段未登记",
	ErrorI18nContent:                "译文超长或含非法字符",
	ErrorI18nResNot:                 "译文目标资源不存在或已被删除",
	ErrorDictTypeNotFound:           "字典类型不存在",
	ErrorDictItemNotFound:           "字典项不存在",
	ErrorDictTypeCodeTaken:          "字典类型编码（code）已存在",
	ErrorDictValueTaken:             "字典项取值（value）在该类型下已存在",
	ErrorDictTypeHasItems:           "字典类型下存在字典项，禁止删除",
	ErrorDictBadParams:              "字典参数不合法",
	ErrorDictTypeDisabled:           "字典类型已停用，其下字典项不下发",
	ErrorLoginConfigNotFound:        "登录配置不存在",
	ErrorLoginConfigBadParams:       "登录配置参数不合法",
	ErrorLoginConfigVersionConflict: "配置已被他人修改，请刷新后重试",
	ErrorLoginConfigKeepPwd:         "管理后台必须保留“账号+密码”登录方式",
	ErrorLoginMethodClosed:          "该登录方式未开放",
	ErrorSelfPwdDisabled:            "管理员已关闭自助修改密码",
}

// GetMsg 获取业务码对应文案，未知码回退通用 ERROR 文案。
func GetMsg(code int) string {
	if m, ok := msgMap[code]; ok {
		return m
	}
	return msgMap[ERROR]
}
