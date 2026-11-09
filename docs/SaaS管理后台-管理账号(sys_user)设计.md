# SaaS 管理后台 · 管理账号（sys_user）设计

> 版本 v1.2 · 范围：**仅管理账号体系**（平台运营方自己的管理后台）
> 创建：2026-11-06 · 最后修改：2026-11-09
> 增量章节：§13（v1.1/v1.2 已上线增量，与实现同步）；§14（边界补充）；§15（部署与运维）
> 规矩：代码变更一律同 commit 写入本文档。
> 租户账号（`hr_account`）本次不设计，见 §12。

---

## 1. 先划界：这是两套账号体系

之前的设计把两套体系混在了一起。这次先划清：

| | `sys_user` 管理账号（本次） | `hr_account` 租户账号（暂不设计） |
|---|---|---|
| 归属 | SaaS 平台运营方自己 | 企业客户 |
| 数量 | 几十人 | 数万起 |
| 权限 | **最大**（能碰所有租户的数据） | 限定在本租户内 |
| 租户概念 | **无** | 有（tenant_id 是核心） |
| 登录方式 | 账号/手机 + 密码；手机 + 短信 | 未来另定（可能含 SSO、多标识） |
| 表结构 | **单表** | 未来独立设计 |

**这次纠偏的核心判断：管理账号用单表就是正确答案。**

之前那套"四表模型、多租户、多标识、SSO"解决的是租户侧的问题——多租户隔离、一人多账号、多手机多邮箱、第三方免登。这些问题在管理账号上**一个都不存在**：

- 没有租户：平台运营方只有一个，账号不挂在任何租户下
- 标识只有两个：账号、手机号，各一个，不可多值
- 没有 SSO：人数少、全是自己人，密码 + 短信足够
- 没有一人多账号：一人一个账号

为不存在的需求建四张表，是纯粹的成本。**但省掉的只是结构复杂度，不能省安全机制**——管理账号数量最少、权限最大，它恰恰是整个系统最值得攻击的目标（拿到一个管理账号 = 看到所有租户的数据）。所以本文的重点不是表结构，是 §5~§8 那些安全机制。

## 2. 设计原则

1. **路由隔离**：管理后台登录全部挂在 `/api/v1/admin/auth/*`。未来租户侧用 `/api/v1/tenant/auth/*`。**两套体系不共用任何登录代码**——它们的安全策略注定分化（管理侧更严），早期共用是省一次复制，一年后是改一处崩一片。
2. **三种入口，一条管线**：账号+密码、手机+密码、手机+短信。前两者是同一个"密码因子"，只是标识不同。
3. **不做清单**：SSO、MFA（预留扩展点）、多手机、多邮箱、自助绑定标识、租户切换。

---

## 3. 数据模型

### 3.1 字段清单与 DDL

| 需求字段 | 表字段 | 说明 |
|---|---|---|
| 姓名 | `name` | 必填 |
| 手机 | `mobile_hash` + `mobile_enc` + `mobile_mask` | 一列拆三列，原因见 §3.3 |
| 邮箱 | `email` | 选填 |
| 账号 | `username` | 必填，**禁止手机号样式**（§3.3） |
| 密码 | `password_hash` | bcrypt，自带盐 |
| 密码盐 | `salt` | **默认不用**，仅旧算法兼容（§3.3） |
| 状态 | `status` | 1 正常 2 停用 3 锁定 |
| 最后登录时间 | `last_login_at` | |
| 登录 IP | `last_login_ip` | |
| 错误次数 | `fail_count` | 配合 `locked_until` |

另外补了 6 个需求里没列、但缺了就跑不起来的字段：`id`、`locked_until`（冻结截止时间，光有错误次数不知道锁到何时）、`pwd_updated_at`（密码过期判定）、`deleted_at`（软删，审计需要）、`created_at` / `updated_at`。

```sql
CREATE TABLE IF NOT EXISTS sys_user (
    id             BIGINT        NOT NULL,
    username       VARCHAR(64)   NOT NULL COMMENT '登录账号；禁止 11 位纯数字，防与手机号歧义',
    password_hash  VARCHAR(128)  NOT NULL COMMENT 'bcrypt；空串=无密码（仅短信登录）',
    password_algo  VARCHAR(16)   NOT NULL DEFAULT 'bcrypt' COMMENT 'bcrypt/sha256（旧算法迁移用）',
    salt           VARCHAR(64)   NOT NULL DEFAULT '' COMMENT '仅旧算法兼容；bcrypt 自带盐，此列留空',
    name           VARCHAR(64)   NOT NULL,
    mobile_hash    CHAR(64)      NOT NULL COMMENT 'HMAC-SHA256(归一化手机号)，登录查询入口',
    mobile_enc     VARCHAR(256)  NOT NULL COMMENT 'AES-GCM 密文，发短信/核对时解密',
    mobile_mask    VARCHAR(20)   NOT NULL COMMENT '138****8000，列表展示',
    email          VARCHAR(128)  NOT NULL DEFAULT '',
    status         TINYINT       NOT NULL DEFAULT 1 COMMENT '1正常 2停用 3锁定',
    fail_count     SMALLINT      NOT NULL DEFAULT 0,
    locked_until   DATETIME(3)   NULL,
    last_login_at  DATETIME(3)   NULL,
    last_login_ip  VARCHAR(45)   NOT NULL DEFAULT '',
    pwd_updated_at DATETIME(3)   NULL,
    deleted_at     BIGINT        NOT NULL DEFAULT 0,
    created_at     DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at     DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_username (username, deleted_at),
    UNIQUE KEY uk_mobile   (mobile_hash, deleted_at),
    KEY idx_user_status (status)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
```

两个唯一键各管一件事：`uk_username` 防账号重复，`uk_mobile` 保证**一个手机号只能是一个人**（手机不支持多值，这条就是它的全部约束）。软删参与唯一键（`deleted_at` 默认 0，删除时写毫秒戳），是 MySQL 没有部分唯一索引的标准替代。

### 3.2 登录日志表

```sql
CREATE TABLE IF NOT EXISTS sys_login_log (
    id               BIGINT        NOT NULL,
    user_id          BIGINT        NOT NULL DEFAULT 0 COMMENT '解析失败时为 0',
    identifier_mask  VARCHAR(64)   NOT NULL COMMENT '脱敏后的输入：ad***in / 138****8000',
    login_type       VARCHAR(16)   NOT NULL COMMENT 'PASSWORD / SMS',
    ident_via        VARCHAR(16)   NOT NULL DEFAULT '' COMMENT '命中的标识：USERNAME / MOBILE',
    success          TINYINT(1)    NOT NULL,
    fail_reason      VARCHAR(32)   NOT NULL DEFAULT '' COMMENT '固定枚举，见 §8',
    ip               VARCHAR(45)   NOT NULL DEFAULT '',
    ua               VARCHAR(512)  NOT NULL DEFAULT '',
    created_at       DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_log_user (user_id, created_at),
    KEY idx_log_time (created_at),
    KEY idx_log_ip   (ip, created_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
```

三条使用规则：

1. **写事务外**。登录失败往往伴随事务回滚，日志必须独立落库，否则"最需要留下的失败记录"跟着消失了。
2. **存脱敏标识，不存原文**。日志表是最常被导出、权限最松的表，明文手机号不该出现在这里。
3. **`fail_reason` 是固定枚举**，不是自由文本：`BAD_CREDENTIAL / LOCKED / DISABLED / CAPTCHA_FAILED / SMS_CODE_ERROR / SMS_EXPIRED / RATE_LIMITED`。自由文本没法做告警和统计。

### 3.3 三个字段的设计说明

**① 手机为什么拆三列**

| 列 | 用途 | 谁用 |
|---|---|---|
| `mobile_hash` | 登录/查询的唯一入口，唯一索引建在它上面 | 登录管线 |
| `mobile_enc` | 发短信、客服核对时解密 | 短信服务 |
| `mobile_mask` | 列表页展示，不解密 | 管理界面 |

一句话：**查询走 hash、使用走密文、展示走脱敏**。如果只存明文，拖库即泄露全部管理员手机号；只存 hash 则没法发短信。三列是这条业务的最小完备集。

**② 密码盐列为什么默认不用**

bcrypt 的输出形如 `$2a$12$<salt><hash>`，**盐已经在输出串里**。单独维护 `salt` 列，通常意味着底层还在用 MD5/SHA+盐——那是要淘汰的算法。

处理方式：列保留但只作旧算法兼容，配合 `password_algo` 分支。如果存量密码是 MD5+盐：登录时按 `algo` 分支校验，**校验成功后透明升级为 bcrypt** 并清空 `salt`，迁移完成后删掉分支。新写入一律 bcrypt，`salt` 留空。

**③ username 为什么禁止手机号样式**

登录标识可以是账号、也可以是手机号。如果允许账号是 `13800001234`，这条输入就既可能命中账号、又可能命中手机号，解析出现歧义。

解法在源头：**创建账号时校验，username 不得是 11 位纯数字**。歧义消除后，解析规则永远是两条、且不会同时命中：

```
① 按 username 精确匹配（唯一索引）
② 未命中且形似手机号 → 归一化（去 +86/空格/横线）→ 按 mobile_hash 匹配
③ 都未命中 → 走"不存在"分支
```

---

## 4. 登录管线

### 4.1 三个入口，一条管线

```
            ┌── 账号 + 密码 ──┐
登录请求 ── ├── 手机 + 密码 ──┤ → ① 解析 user（只判存在）
            └── 手机 + 短信 ──┘        ↓
                                  ② 因子校验（密码 或 短信码）
                                       ↓
                                  ③ 状态判定（停用/锁定）
                                       ↓
                                  ④ 签发会话 + 写日志 + 清计数
```

注意两个顺序，它们是安全边界：

- **状态判定在因子校验之后**。提前拒绝"该账号已停用"，等于向一个没有密码的人确认"这个账号存在、而且被停用了"。
- **失败计数按 `user_id` 计，不按标识分桶**。账号密码错 3 次、换手机号再错 3 次，必须是同一本账——否则换标识等于重置计数，锁定形同虚设。

### 4.2 接口形态

```
POST /api/v1/admin/auth/login/password   {identifier, password, captcha_token?}
POST /api/v1/admin/auth/login/sms        {mobile, code}
```

两个接口而不是一个：密码和短信码的请求体差异太大，硬塞进一个 DTO 会得到一堆 `if loginType == ...`。但**两条路径在 ① 解析之后汇合到同一段管线代码**（状态判定、计数、日志、签发都只有一份），这是"入口可以分、管线不能分"。

---

## 5. 短信验证码

| 项 | 规则 | 为什么 |
|---|---|---|
| 格式 | 6 位数字，5 分钟有效，**一次性** | 用后即删，防重放 |
| 前置 | 发送必须先过图形验证码 | 短信轰炸是真实资损 |
| 限流 | 单手机号 10 条/天、1 条/分钟；单 IP 100 条/天 | 双重兜底 |
| 试错 | 单个验证码错 5 次即作废 | 防爆破（6 位码只有 100 万种） |
| 用途隔离 | Redis key 带用途：`sms:code:LOGIN:{hash}` / `sms:code:RESET:{hash}` | 登录码不能用来改密码 |
| 存储 | **只存 Redis，不落库** | 高频写 + 自动过期，数据库干不来 |
| 防枚举 | 手机号不存在 → 照样返回"已发送"，实际不发，日志记 `SEND_SKIPPED` | 见下 |

防枚举这条对管理后台尤其重要：**管理后台的手机号清单 = "谁是平台管理员"**，这本身就是高价值情报。攻击者批量试手机号，接口若如实返回"该手机号未注册"，等于把管理员名单送出去。

---

## 6. 失败计数与锁定

| 项 | 规则 |
|---|---|
| 计数维度 | `user_id`（主）+ IP（撞库兜底，50 次/小时） |
| 阈值 | 连续错 5 次 → 锁 15 分钟 |
| 锁定期内 | 拒绝并返回剩余秒数；**不重置计时**（否则每次尝试都在续期） |
| 成功登录 | 清零 `fail_count`、清 `locked_until` |
| 存储 | Redis 计数（`INCR`+`EXPIRE`）+ `sys_user.fail_count / locked_until` 双写 |

Redis 与 DB 的关系必须写死：**Redis 是加速，DB 是兜底**。Redis 查询失败时回落到 DB 判断，绝不允许写成：

```go
// ✗ 这一行的意思是"Redis 挂了就放行"——等于给攻击者一个
//   "打挂 Redis 即可绕过锁定"的开关
locked, err := redis.IsLocked(uid)
if err != nil { return false }
```

另外：**停用账号的登录尝试不计失败次数**。否则一个已停用的账号被人反复尝试，会把 `fail_count` 灌满，等员工复职时账号处于锁定态，还查不出原因。

---

## 7. 会话

管理后台用**最简单的会话模型**，不引入 refresh token 轮转那套机制：

```
登录成功 → 签发 JWT（sub=user_id, jti, exp） + Redis 会话键
           key:  sess:{jti}
           TTL:  8 小时滑动（每次请求续期），硬上限 24 小时
登出     → 删 Redis 键
改密码/停用账号 → 删该 user 全部会话键（秒踢所有端）
```

- JWT 只带 `user_id` 和 `jti`，**权限不进 token**——每次请求查 Redis 会话有效即放行。这样"改密码踢下线""停用即失效"是删一个 key 的事，不需要等 token 自然过期。
- 硬上限 24h：防止一个被劫持的会话靠持续活动永不过期。
- 为什么不用 refresh 轮转：管理后台是低频、高信任场景，滑动会话 + 可秒踢已经覆盖了它的威胁模型。轮转是为"token 可能被长期持有"的场景设计的，这里不成立。

---

## 8. 安全清单（必须做对的七件事）

1. **防时序枚举**：账号不存在时用固定假 hash 陪跑一次 bcrypt。否则"响应快 80ms"就能批量探测哪些账号存在。
2. **bcrypt cost ≥ 12**；密码策略 ≥ 10 位、至少含两类字符；`pwd_updated_at` 超 180 天登录时提示改密（不强制）。
3. **超管保护**：内置超管账号不可停用、不可删除、必须密码登录（防"短信通道故障导致平台失管"）、对其账号的一切操作强制审计。
4. **错误文案统一**：对外只有一句"账号或密码错误"，不区分账号不存在 / 密码错 / 已停用。
5. **登录日志写事务外**（§3.2）。
6. **接口限流**：登录接口 IP 维度 20 次/分钟；短信发送另有 §5 的独立配额。
7. **图形验证码**：短信发送必带；密码登录连续失败 3 次后必带。

---

## 9. 接口清单

```
POST /api/v1/admin/auth/captcha             获取图形验证码
POST /api/v1/admin/auth/sms/send            发送登录短信（需图形码）
POST /api/v1/admin/auth/login/password      账号或手机 + 密码
POST /api/v1/admin/auth/login/sms           手机 + 短信码
POST /api/v1/admin/auth/logout              登出当前会话
GET  /api/v1/admin/auth/me                  当前用户信息

-- 账号管理（管理后台的"管理"）
GET    /api/v1/admin/sys-users              列表（手机返回 mask）
POST   /api/v1/admin/sys-users              创建
PUT    /api/v1/admin/sys-users/{id}         修改
DELETE /api/v1/admin/sys-users/{id}         停用（软删）
POST   /api/v1/admin/sys-users/batch-freeze      批量冻结（v1.1，契约见 §13.1）
POST   /api/v1/admin/sys-users/batch-unfreeze    批量解冻（v1.1，契约见 §13.1）
（v1.1 移除：POST /sys-users/{id}/unlock，由 batch-unfreeze 替代）
POST   /api/v1/admin/sys-users/{id}/reset-password   重置密码（强审计）
GET    /api/v1/admin/login-logs             登录日志查询（v1.2：identifier/到 HH:mm:ss/默认当天）
GET    /healthz                             存活探针（白名单，不探依赖；见 §15）
```

---

## 10. 边界情况清单

| 情况 | 处理 |
|---|---|
| 账号不存在 | 陪跑 bcrypt；按标识哈希在 Redis 兜底计数；返回统一文案 |
| 账号停用 | 因子校验通过后拒绝，`fail_reason=DISABLED`；**不计失败次数** |
| 账号锁定中 | 拒绝并返回 `remaining_seconds`；不重置锁定计时 |
| 密码错误且已有图形码要求 | 先校验图形码，失败 `CAPTCHA_FAILED`，同样计数 |
| 短信码错误 | 计数 +1；错满 5 次验证码作废，需重新发送 |
| 短信码过期/已用 | 返回 `SMS_EXPIRED`，引导重发 |
| 发送短信给未注册手机号 | 返回"已发送"，实际不发，日志记 `SEND_SKIPPED` |
| username 输入了 11 位数字 | 正常：先按账号精确匹配（不会命中），再按手机号匹配 |
| 同一手机号试图注册第二个账号 | `uk_mobile` 拒绝。管理账号一人一号，没有例外 |
| 登录成功但写日志失败 | **不影响登录结果**（日志异步写），但要在监控里告警 |
| Redis 挂掉 | 锁定判断回落 DB（`locked_until`）；验证码功能不可用则短信登录暂不可用并提示 |
| 修改密码 / 停用账号 | 删除该用户全部会话键，全端秒踢 |

---

## 11. 实施顺序

| 阶段 | 内容 |
|---|---|
| **P0** | 两张表 + 迁移；密码登录（账号/手机标识）；失败计数与锁定；会话签发与秒踢；登录日志 |
| **P1** | 短信登录 + 图形验证码 + 发送限流 |
| **P1.5** | 账号管理 CRUD + 重置密码 + 解锁（管理后台总得先能建账号） |
| **P2（可选）** | TOTP 二次认证。管理账号权限大，值得加，但先让 P0~P1.5 跑起来 |

P0 的验收标准：**一个初始超管账号能登录，输错 5 次会被锁，日志里有记录**。这一条立住了，后面都是增量。

---

## 12. 租户账号（hr_account）留给未来

本次不设计，但划界先立住：

1. **它是一套独立的表和独立的路由**，不是 `sys_user` 加 `tenant_id`。两者的生命周期、安全策略、规模都不同。
2. 之前讨论过的一批结论（标识与账号分离、多标识唯一键设计、SSO 只负责换 user_id、密码策略取最严等）属于租户侧问题，等做 `hr_account` 时再启用。
3. 两套体系之间的唯一合法通道是"平台员工代客操作"这类**显式授权功能**（要审计、要限时），绝不是共享登录代码或共享表。
## 13. 已上线增量（v1.1/v1.2，与实现同 commit 登记）

### 13.1 批量冻结 / 批量解冻

```
POST /api/v1/admin/sys-users/batch-freeze     { "ids": ["...","..."], "minutes": 30 }
POST /api/v1/admin/sys-users/batch-unfreeze   { "ids": ["...","..."] }
```

| 项 | 规则 |
|---|---|
| ids | 字符串数组，≤100 条；单条解析失败按 `30025` 逐项返回，不打断批 |
| minutes | 仅 batch-freeze：1~10080，缺省 60 分钟，超上限收敛 |
| 响应 | `{items:[{id,success,code?}],succeeded,failed}`；**逐项独立**，部分失败不影响已成功项 |
| 冻结语义 | `locked_until = now + minutes` + `status=3`（DB 权威）+ Redis 锁标记（加速层）+ 全端秒踢（§7）；Redis 故障回落 DB 规则同 §6 |
| 解冻语义 | 清 `fail_count`/`locked_until`、状态回 1、清 Redis 计数与锁标记 |
| 与 §6 自动锁定的关系 | “连错 5 次锁 15 分钟”是自动安全锁定；人工冻结默认 60 分钟，复用 `locked_until` 字段，语义并行 |
| 管理红线 | 超管不可被任何人冻结（`30026`，§8.3）；冻结/解冻不可操作自己（`30027`） |
| 幂等 | 冻结已冻结者=顺延锁定期；解冻非冻结者=无害（清计数仍执行） |
| 审计 | 每条成功项独立审计 `sysuser.freeze/unfreeze`（操作者/目标/时长/到期/来源 IP） |
| 接口变更 | **移除** `POST /sys-users/{id}/unlock`，由 batch-unfreeze 替代 |

### 13.2 鉴权过滤与白名单

- 全局统一过滤 `AuthGuard`：白名单外任何请求必须携带有效会话——**安全默认 deny**，新增接口不配置即被拦；
- 白名单 `routes.PublicPaths`：`/healthz`、`auth/captcha`、`auth/sms/send`、`auth/login/password`、`auth/login/sms`；`logout/me` 与全部管理面须登录；
- 鉴权链：JWT（HS256，仅 uid/jti）→ Redis 会话（删键即踢）→ 归属一致校验 → 8h 滑动续期（24h 硬上限）。

### 13.3 图形验证码

- `GET`/`POST` 双开（地址栏与 `<img>` 场景天然 GET；签发是免费一次性资源）；
- 签发防刷（IP）：10/分钟 + 60/小时双窗口，超限 `30017`；
- 答案 5 位数字，一次性（GetDel）、5 分钟有效，存储在 Redis。

### 13.4 登录输入快筛（“达标才校验”）

| 输入 | 处理 |
|---|---|
| 密码 <6 位 / >128 | `30024`，不进 DB/Redis/bcrypt |
| identifier 空/超长/控制字符 | `30025` |
| 短信码非 6 位数字 | `400`，不消耗一次性码与试错计数 |
| 图形码答案非 5 位数字 | `30013`，不碰 Redis |

快筛判据与账号是否存在无关（同等拒绝），不构成存在性枚举信号。

### 13.5 自操作防线与 is_current

- **自操作**：停用/启用、删除、冻结/解冻的目标=当前登录人 → 拒绝（`30027`，应用层与接口层双保险）；
- **is_current**：`sys_user` 列表、登录响应、`auth/me` 的 user 视图逐行/恒定标注“是否当前登录人”，前端据此灰 checkbox、隐藏自我操作按钮。

### 13.6 语言中立响应合同

| 项 | 约定 |
|---|---|
| 雪花 ID | 字符串（超 JS 2^53，JSON 库大整型行为差异） |
| 时间 | `"yyyy-MM-dd HH:mm:ss"` 字符串 |
| 手机号 | mask（138****0000）；明文只在发短信内部路径 |
| 枚举 | 大写字符串（PASSWORD/SMS/USERNAME…） |
| 校验失败(400) | data=非法字段名数组（camelCase），不泄漏 Go 结构名/校验 tag |
| 404/405 兜底 | JSON 回显 `{method,path}` 并双语提示 |
| 多语言 | 请求级 `?lang=`/`Accept-Language`；默认链 yaml i18n.defaultLang > SAAS_LANG > zh-CN |

### 13.7 登录日志（v1.2）

| 项 | 规则 |
|---|---|
| 标识落库 | **账号原样**、手机号归一化 mask（138****8000）；列名暂为 `identifier_mask`（v1.3 若重命名需 breaking 迁移评审，见 §16） |
| 标识查询 | `?identifier=<原始输入>` 同规则转换后精确匹配；兼容传旧 mask（旧 ad***in 不可逆推） |
| 时间范围 | `start_at/end_at`：`YYYY-MM-DD HH:mm:ss` 或纯日期（兼容），**秒级、不收毫秒**（含小数点 → `400`，防御 Go time.Parse 隐式接受小数秒）；纯日期 end 含当日全天 |
| 默认窗口 | start/end 都不传 → **只查当天**（00:00~23:59:59.999） |
| 索引 | 新库 DDL 自带 `idx_log_identifier(identifier_mask, created_at)`；存量表由 v1.2 起“幂等增量索引”机制（information_schema 检查 → CREATE INDEX；新索引在 indexSpecs 登记） |
| 叠加 | user_id/ip/success 与时间/标识可任一组合，走 idx_log_* 原三条索引 |

### 13.8 安全修复

- `SetTrustedProxies(nil)`：修复 gin 默认信任所有代理头 → 伪造 `X-Forwarded-For` 绕过三层 IP 限流的漏洞；部署 nginx/SLB 后改受信代理白名单；
- 基础安全响应头：`nosniff` / `DENY` / `no-store`。

## 14. 边界情况补充（v1.1/v1.2，叠加 §10）

| 情况 | 处理 |
|---|---|
| 批量冻结含“自己” | 该条 `30027`，其余条目正常处理 |
| 批量冻结含“超管 admin” | 该条 `30026`，其余正常 |
| 冻结 minutes 缺省/越界 | 缺省 60 分钟；>10080 收敛上限，不报错 |
| 对已冻结用户再次冻结 | 顺延：`locked_until` 重置到新时刻 |
| 解冻非冻结状态用户 | 无害：清计数执行，状态回 1 |
| 登录日志 start/end 都不传 | 只查当天（§13.7） |
| 时间参数带毫秒/小数点 | `400`（不收毫秒） |
| identifier 传旧 mask | 兼容命中历史行 |

## 15. 部署与运维（v1.2 登记）

### 15.1 初始超管账号

- 仅当 `sys_user` 表**为空**时 seed：username 固定 `admin`；密码/手机/邮箱取 `config/locales/config.yaml` 的 `bootstrapAdmin`（dev 默认 `HrSaas@2026#Admin` / `13800000000`）；
- 生产必须环境变量覆盖：`SAAS_BOOTSTRAP_ADMIN_PASSWORD/_MOBILE/_NAME/_EMAIL`；种子密码须满足密码策略，否则启动失败；
- seed 只对空库生效，**永不覆盖**已有账号；改密走 reset-password（强审计）。

### 15.2 配置与环境变量

| 变量 | 用途 |
|---|---|
| `SAAS_MYSQL_DEFAULT_HOST/PORT/NAME/USER/PASSWORD` | MySQL（dbName 固定 `dugu-hr-saas-admin`） |
| `SAAS_REDIS_HOST/PORT/USERNAME/PASSWORD/DB` | Redis |
| `SAAS_JWT_SECRET` / `SAAS_MOBILE_HMAC` / `SAAS_MOBILE_AES` | 会话签名 / 手机号 HMAC / AES-256（64 位 hex）；prod 必须覆盖（默认 dev 密钥在 prod 拒绝启动） |
| `SAAS_BOOTSTRAP_ADMIN_*` | 初始超管 |
| `SAAS_ADDR` / `SAAS_DEBUG` | 监听地址 / SQL 日志 |
| `SAAS_LANG` / yaml `i18n.defaultLang` | 默认语言（请求级参数优先） |

### 15.3 服务管理脚本

| 命令 | 行为 |
|---|---|
| `make start` / `bash scripts/start.sh` | 幂等后台启动（pid 文件）、可选编译（SKIP_BUILD=1）、固定项目根为工作目录、/healthz 轮询就绪（15s），失败打印日志尾部 |
| `make stop` / `bash scripts/stop.sh` | PID 定位 → SIGTERM 优雅关停（10s 超时 SIGKILL）→ 确认端口释放 |
| `make restart` / `make status` | 重启 / 状态（pid + healthz） |
| 日志 | `logs/run-YYYY-MM-DD.log` 按天切分 |
| `bash scripts/clear-ratelimit.sh [ip|--all]` | 开发期一键清限流计数器（不动验证码/短信码本体；**生产禁用**） |

### 15.4 启动纪律与演进

- 二进制**必须以项目根为工作目录**启动（`config/locales` 相对路径）——start.sh 已强制；
- 迁移每次启动幂等执行（IF NOT EXISTS + information_schema 增量索引检查）；
- 多副本部署演进：把 `Migrate/Seed` 拆为部署期子命令 + 版本化迁移（`schema_migrations`），出现第二个增量 DDL 时引入。

## 16. 演进备忘

- `identifier_mask` 列名与语义已脱节（v1.2 账号原样），v1.3 可考虑改名（MySQL 8 ALGORITHM=INPLACE）并同步接口字段；
- 前端 token 存放（当前 JSON 返回、前端自管）——如改后端 HttpOnly Cookie 需另行设计；
- 密码快筛下限 6 位为兼容旧弱口令；存量全部完成 bcrypt 升级后可收紧至 10。
