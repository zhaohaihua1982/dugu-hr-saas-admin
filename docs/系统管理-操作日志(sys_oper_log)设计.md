    # SaaS 管理后台 · 操作日志（sys_oper_log）设计

> 版本 v1.1 · 范围：**管理后台操作的后审计**（谁 / 何时 / 哪个模块 / 做了什么 / 结果如何）
> 创建：2026-11-09 · 最后修改：2026-11-09
> v1.1 变更：查询操作纳入记录（参数+总数轻量记录）；删除区分逻辑/物理；修改记录差异项。
> 关联：登录事件由 `sys_login_log` 覆盖（见 管理账号设计 §3.2），本文不重复覆盖。

---

## 1. 先划界：三类"记录"各管一段

| | 登录日志 `sys_login_log`（已有） | **操作日志 `sys_oper_log`（本文）** | 应用运行日志（logs/） |
|---|---|---|---|
| 回答的问题 | 谁在何时登录成功/失败 | **谁在何时对哪个模块做了什么操作、结果如何** | 程序内部发生了什么 |
| 触发点 | 登录/登出管线内显式记录 | 路由层中间件自动拦截 | 代码里手打日志 |
| 覆盖对象 | 管理员、未来租户账号 | 仅 `/api/v1/admin/*` | 全部 |
| 面向谁 | 安全分析（撞库、爆破） | **运营审计、追责、复盘** | 排障 |

记录粒度按操作类型分四档（v1.1 定稿）：

| 操作 | 记录内容 | 档位 |
|---|---|---|
| **查询**（GET） | **查询参数 + 返回总数**，仅此两项 | 最轻 |
| **添加** | 添加参数（请求体，脱敏） | 轻 |
| **删除** | 逻辑删除记 **id**；物理删除记 **整行数据 JSON** | 中 |
| **修改** | 请求参数 + **差异项**（改了哪个字段、旧值→新值） | 最重，也是最有审计价值的 |

## 2. 设计原则

1. **默认拦截，显式登记**。中间件挂在 AuthGuard 之后，对 `/api/v1/admin/*` 自动记录——新增接口忘了登记，也会以 `module=unknown` 被记下来（与鉴权白名单"安全默认 deny"同一哲学）。
2. **旁路写入，绝不影响业务**。日志写入失败只打 error 日志，不重试阻塞、不改变业务响应码。
3. **拦截层为主，业务附加为辅**。查询参数、请求体、路径 id、响应码/总数中间件自动捕获；中间件拿不到的（物理删除的整行、修改前后的差异）由业务 handler 通过统一 API 附加，**未附加时兜底记参数，不报错**。
4. **快照自包含**。操作者名字存操作时快照，用户后来改名甚至被删，历史日志仍可读。
5. **只增不改**。不提供修改/删除接口（代码层面就没有 delete handler）。
6. **脱敏优先**。请求参数先脱敏再落库（password / 验证码 / token 类掩码）。

## 3. 数据模型

### 3.1 DDL（config/sql/sys_oper_log.sql）

```sql
-- 业务参考: docs/系统管理-操作日志(sys_oper_log)设计.md
-- 迁移幂等：CREATE TABLE IF NOT EXISTS，启动时执行。
CREATE TABLE IF NOT EXISTS `sys_oper_log` (
    `id`             BIGINT        NOT NULL COMMENT '雪花ID，应用侧生成',
    `operator_id`    BIGINT        NOT NULL DEFAULT 0 COMMENT '操作者 sys_user.id；身份异常时为 0',
    `operator_name`  VARCHAR(64)   NOT NULL DEFAULT '' COMMENT '操作时用户名快照',
    `module`         VARCHAR(32)   NOT NULL COMMENT '模块码：sys-user / login-log / unknown / ...',
    `action`         VARCHAR(32)   NOT NULL COMMENT '动作码：list / query / create / update / delete / unlock / reset-password / ...',
    `method`         VARCHAR(8)    NOT NULL COMMENT 'HTTP 方法',
    `route`          VARCHAR(128)  NOT NULL DEFAULT '' COMMENT 'gin 路由模板，如 /api/v1/admin/sys-users/:id',
    `path`           VARCHAR(256)  NOT NULL DEFAULT '' COMMENT '实际请求路径（含真实 id）',
    `target_id`      VARCHAR(64)   NOT NULL DEFAULT '' COMMENT '路径参数 :id',
    `detail`         VARCHAR(2048) NOT NULL DEFAULT '' COMMENT '操作明细 JSON，按动作类型形状不同，见 §3.2',
    `biz_status`     INT           NOT NULL DEFAULT 0 COMMENT '业务响应码 {status} 的值',
    `success`        TINYINT(1)    NOT NULL COMMENT '业务是否成功（status==0）',
    `cost_ms`        INT           NOT NULL DEFAULT 0 COMMENT '处理耗时毫秒',
    `ip`             VARCHAR(45)   NOT NULL DEFAULT '',
    `ua`             VARCHAR(512)  NOT NULL DEFAULT '',
    `created_at`     DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    KEY `idx_oper_user` (`operator_id`, `created_at`),
    KEY `idx_oper_mod`  (`module`, `created_at`),
    KEY `idx_oper_time` (`created_at`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
```

- `detail` 是**统一明细列**（v1.0 的 `req_summary` 升级）：不同动作存不同形状的 JSON（§3.2），而不是每种操作加一列——审计是长尾需求，形状会继续演化，一列 JSON 最省事。
- `biz_status` 存业务码而非 HTTP 状态码：本项目 HTTP 恒 200，成败语义在 `{status}` 里。

### 3.2 detail 的四种形状（对应 §1 四档）

| 动作 | detail 形状 | 数据来源 |
|---|---|---|
| 查询 list/query | `{"params": "<查询参数>", "total": 123}` | **中间件自动**：params 取 raw query string；total 取响应 `data.total`（解析不到记 0） |
| 添加 create | `{"params": {...}}`（脱敏 + 2048 截断） | **中间件自动** |
| 修改 update | `{"params": {...}, "diff": {"字段": {"old": x, "new": y}}}` | params 中间件自动；**diff 业务附加**（§4.6） |
| 删除 delete | 逻辑：`{"id": "123"}`；物理：`{"row": {整行数据}}` | 逻辑删除 id 从路径参数自动取；**物理删除整行业务附加**（§4.6） |

设计要点：

- **查询记 raw query string**（`page=1&page_size=20&keyword=x`）而不是解析后的对象——省解析成本，且人直接可读；`total` 从响应体快照里取 `data.total`，前端分页信息天然在里面。
- **逻辑 vs 物理删除由登记表声明**（§4.2 registry 加 `PhysDelete: bool`），同一 action 码下中间件决定记 id 还是等业务附加整行。
- **diff 是可选增强**：业务 handler 没附加 diff 时，update 只记 params——审计不因为业务偷懒而断档。

### 3.3 与 sys_login_log 的边界

`/api/v1/admin/auth/*` **整体跳过**（含 GET /me）：登录成败由 `sys_login_log` 记录；`me` 属会话状态查询，无审计价值。避免同一事件两处记录。

## 4. 拦截机制

### 4.1 装配位置

```
gin.Recovery → Cors → SecureHeaders → AuthGuard（设 uid/jti）→ OperLog → 路由
```

OperLog 必须在 AuthGuard 之后：拿不到 uid 就没法记录"谁"。

### 4.2 元数据登记表（唯一的"登记成本"）

```go
// routes/oper_meta.go
type OperMeta struct {
    Module     string // 模块码，如 "sys-user"
    Action     string // 动作码，如 "create" / "list"
    PhysDelete bool   // 仅 delete：true=物理删除（业务附加整行），false=逻辑删除（记 id）
}

// key = method + gin 路由模板（c.FullPath() 的返回值）
var OperMetaRegistry = map[string]OperMeta{
    "GET /api/v1/admin/sys-users":                     {Module: "sys-user", Action: "list"},
    "POST /api/v1/admin/sys-users":                    {Module: "sys-user", Action: "create"},
    "PUT /api/v1/admin/sys-users/:id":                 {Module: "sys-user", Action: "update"},
    "DELETE /api/v1/admin/sys-users/:id":              {Module: "sys-user", Action: "delete"}, // 逻辑删除
    "POST /api/v1/admin/sys-users/:id/unlock":         {Module: "sys-user", Action: "unlock"},
    "POST /api/v1/admin/sys-users/:id/reset-password": {Module: "sys-user", Action: "reset-password"},
    "GET /api/v1/admin/login-logs":                    {Module: "login-log", Action: "list"},
    // GET /api/v1/admin/oper-logs 不登记：查操作日志本身会产生新日志，套娃无意义，显式跳过
}
```

未命中的请求兜底记为 `module=unknown, action=<method>`——宁可记得难看，不可漏记；`unknown` 出现即说明有人加了接口忘了登记。

### 4.3 中间件处理流程

```
1. 路径前缀是 auth/ ？是 → 跳过
2. start := now；换装 teeWriter（捕获响应前 512 字节）
3. 若为写方法且带 body：缓存读取请求体（回填 c.Request.Body，保证 handler 正常 bind）；
   GET 则取 raw query string
4. c.Next() 跑完业务
5. 组装记录：uid/name 快照、FullPath()→registry 查 module/action/PhysDelete
   （未命中→unknown）、detail 按 §3.2 形状组装（含业务附加合并，§4.6）、
   biz_status/success/total 从响应快照解析、cost_ms、ip、ua
6. 投递到异步 channel，立即返回不阻塞响应
```

multipart/form-data 不记 body，只记"（multipart）"占位。

### 4.4 异步写入

```go
ch := make(chan *OperRecord, 1024)   // 有界
go consumer(ch)                       // 单消费协程批量落库
// 投递：select default 放弃 + 丢弃计数器打 warn —— 满了宁丢不阻
```

有界 + 丢弃计数必须写进代码，这是"旁路绝不影响业务"的兜底。进程优雅关闭时 drain channel。

### 4.5 脱敏规则

对 `detail` 里的 params 递归处理，命中的 key 整值替换为 `"***"`：

```
password, old_password, new_password, confirm_password,
captcha, code, sms_code, token, secret, key
```

匹配不区分大小写、支持包含（如 `captcha_code`）。常量放 `middleware/operlog` 包内，新增敏感字段只改一处。

### 4.6 业务附加机制（v1.1 新增，物理删除整行 / 修改差异的唯一通道）

```go
// middleware/operlog —— 供业务 handler/service 调用
// 附加明细，中间件组装 detail 时合并进去（同名 key 以业务附加为准）
operlog.SetDetail(c, map[string]any{"row": entity})           // 物理删除前：整行序列化
operlog.SetDetail(c, map[string]any{"diff": operlog.Diff(old, new)})
```

```go
// Diff：反射对比两个同构对象，产出差异项
// 返回 map[string]{"old": x, "new": y}，只含有差异的导出字段
func Diff(oldVal, newVal any) map[string][2]any
```

使用约定：

- **物理删除**：handler 在执行删除**之前**取出整行并 `SetDetail(c, ...)`——删完就查不到了，顺序不能反；
- **修改**：service 层先读旧行，与请求参数对比，`Diff()` 产出差异后 `SetDetail(c, ...)`；没有"读旧行"条件的场景（如纯条件更新）允许不附加，兜底只记 params；
- `Diff` 的反射实现约定：只比对导出字段、`time.Time` 按值比较、切片/结构体整体比较不做深 diff（够用且不会错）；
- 中间件组装 detail 时：`业务附加` 与自动采集（params）合并，同名 key 以附加为准；**未附加不报错不告警**，这是可选增强不是义务。

## 5. 查询接口

```
GET /api/v1/admin/oper-logs
    ?page=&page_size=&operator_id=&module=&action=&success=&start=&end=&keyword=
```

- 响应：`{status, msg, data:{list, total, page, page_size}}`，按时间倒序；
- `keyword` 模糊匹配 `path` / `operator_name`；
- 该接口自身**不产生操作日志**（registry 不登记 + 路径跳过，双保险）。

**刻意不做**：删除、修改、批量清空接口。保留策略（表增长）用定期归档解决——按 `created_at` 搬 N 个月前数据到归档表/文件，属运维动作，不进业务 API。

## 6. 前端与多语言

- 页面：`系统管理 → 操作日志`，列表列 = 时间 / 操作人 / 模块 / 操作 / 对象ID / 结果 / 耗时 / IP；行展开看 `detail`；
- `detail` 渲染按动作类型区分：查询显示参数与总数；修改把 `diff` 渲染为「字段：旧值 → 新值」表；物理删除显示整行 JSON（格式化）；
- `module`/`action` **存码不存文案**：前端语言包翻译（与数据文案多语言同一思路的两半：一个存码、一个翻展示）；未知码兜底显示 `method + path`，界面不许出现裸 key；
- 结果列：成功绿 / 失败红，失败悬浮显示 `biz_status` 对应的错误文案。

## 7. 边界情况清单

| 情况 | 处理 |
|---|---|
| 业务失败（status≠0） | **照记**，success=0——失败尝试恰是审计重点（越权尝试、刷接口） |
| 查询响应无 total（如导出类接口） | total 记 0，params 照记 |
| detail 超 2048B | 截断 + `"…truncated"` 标记 |
| multipart 请求 | 不记 body，记占位符 |
| 物理删除未附加整行 | 兜底只记 id（target_id），不报错；属应修事项 |
| update 未附加 diff | 兜底只记 params，不报错 |
| AuthGuard 之后 uid 异常缺失 | operator_id=0 照记，排查中间件装配 |
| 未登记的新接口 | module=unknown 照记（§4.2 兜底） |
| 日志通道满 | 丢弃 + 丢弃计数 warn，业务无感 |
| 日志落库失败 | error 日志，业务无感，不重试阻塞 |
| auth/* 路径 | 整体跳过（sys_login_log 覆盖登录事件） |
| oper-logs 查询自身 | 不记（防套娃） |

## 8. 实施顺序

1. **P0**：DDL + `middleware/operlog`（teeWriter、请求体缓存、脱敏、异步通道、`SetDetail`/`Diff`）+ `oper_meta.go` 登记现有全部路由（含 GET）+ auth 跳过规则。验收：增删改查用户后表里记录形状符合 §3.2；reset-password 的 detail 中 password 字段已掩码；业务接口行为无任何变化。
2. **P1**：sys-users 的 update 接入 `Diff`（service 层读旧行→Diff→SetDetail）；查询接口 `GET /oper-logs`（含错误码、双语言包）+ 前端列表页（diff 渲染、接 i18n 机制）。
3. **P2**：后续新增模块随路由登记元数据；物理删除类接口（将来清字典项等）按 §4.6 约定附加整行。写进 AI 开发规范自检清单：「新增 admin 接口必须同步登记 OperMetaRegistry；物理删除必须在删除前 SetDetail 整行」。
