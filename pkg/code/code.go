// Package e 业务错误码。编号按“模块×10000”分段，与 gin-mall 的 pkg/e 同风格；
// 30000 段保留给 SaaS 管理后台认证体系。
package code

const (
	SUCCESS       = 200
	UpdateSuccess = 201
	ERROR         = 500
	InvalidParams = 400

	// ---------- 认证（30001~30030） ----------
	ErrorAuthCheckTokenFail = 30001 // token 无效/过期/会话失效
	ErrorAuthRequired       = 30002 // 未登录
	ErrorBadCredential      = 30010 // 账号或密码错误（统一文案，不区分细分原因）
	ErrorUserLocked         = 30011 // 账号锁定中（data.remaining_seconds）
	ErrorUserDisabled       = 30012 // 账号已停用
	ErrorCaptchaFailed      = 30013 // 图形验证码错误或已过期
	ErrorCaptchaRequired    = 30014 // 需要图形验证码（连续失败达阈值）
	ErrorSmsCodeError       = 30015 // 短信验证码错误
	ErrorSmsExpired         = 30016 // 短信验证码过期/已用
	ErrorRateLimited        = 30017 // 请求过于频繁
	ErrorSendSms            = 30018 // 短信发送失败

	// ---------- 管理操作（30021~30040） ----------
	ErrorUserNameTaken       = 30021 // 账号已存在
	ErrorMobileTaken         = 30022 // 手机号已绑定其他账号
	ErrorSysUserNotFound     = 30023 // 管理账号不存在
	ErrorWeakPassword        = 30024 // 密码不满足策略
	ErrorBadIdentifierFormat = 30025 // 账号/手机号格式不合法
	ErrorSuperAdminProtected = 30026 // 超管账号受保护，禁止该操作
	ErrorSelfOperation       = 30027 // 不能对当前登录账号自身执行停用/启用、解锁等操作

	// ---------- 菜单（30031~30036，docs 三模块设计 §5） ----------
	ErrorMenuNotFound        = 30031 // 菜单不存在
	ErrorMenuHasChildren     = 30032 // 菜单存在子节点，禁止删除/改类型
	ErrorMenuActionCodeTaken = 30033 // 动作编码(action_code)已被占用
	ErrorMenuBadParams       = 30034 // 菜单参数不合法（字段/父节点/环/深度）
	ErrorMenuLangUnsupported = 30035 // 语言不被支持（names key 须为已注册语言）
	ErrorMenuNameRequired    = 30036 // 默认语言名称（主表 name）不能为空
	// 30033 已废弃：全局唯一改为同级唯一后并入 30037（保留编号防止前端旧映射错乱）
	ErrorMenuSameLevelTaken = 30037 // 同级已存在相同名称/请求地址/权限点（v0.3）

	// ---------- 数据文案翻译（30038~30040，docs 基础设施-数据文案多语言(i18n)设计.md §13） ----------
	ErrorI18nTarget  = 30038 // 翻译资源类型/字段未登记（白名单外，禁止翻译标识字段）
	ErrorI18nContent = 30039 // 译文超长或含非法字符（控制字符、\x00）
	ErrorI18nResNot  = 30040 // 译文目标资源不存在（resType 合法但 resId 查无此行/已逻辑删除，v1.2）

	// ---------- 字典（30041~30047，docs 系统管理-字典(sys_dict)设计.md §4；30048~30049 预留） ----------
	ErrorDictTypeNotFound  = 30041 // 字典类型不存在
	ErrorDictItemNotFound  = 30042 // 字典项不存在
	ErrorDictTypeCodeTaken = 30043 // 字典类型编码（code）已存在
	ErrorDictValueTaken    = 30044 // 字典项取值（value）在该类型下已存在
	ErrorDictTypeHasItems  = 30045 // 字典类型下存在字典项，禁止删除
	ErrorDictBadParams     = 30046 // 字典参数不合法（code/value 格式、长度、typeCode 不存在等）
	ErrorDictTypeDisabled  = 30047 // 字典类型已停用，其下字典项不下发（withDisabled=1 管理通道放行）

	// ---------- 路由兜底 ----------
	ErrorNotFoundNoRoute = 404 // 接口不存在或请求方式不对（GET 访问了 POST 接口等）

	// ---------- 数据库（40001） ----------
	ErrorDatabase = 40001
)
