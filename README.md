# dugu-hr-saas-admin · SaaS 管理后台（标准 DDD）

SaaS 平台运营方自己的管理后台账号体系（`sys_user`），业务设计见
[docs/SaaS管理后台-管理账号(sys_user)设计.md](docs/SaaS管理后台-管理账号(sys_user)设计.md)。
范围：**P0~P1.5**。架构为**严格按照标准 DDD 四层**实现，不混杂非 DDD 结构。

## 分层与依赖方向（依赖倒置：下层不知道上层）

```
interfaces（api/ routes/ middleware/）
        │  只持有 application 用例的引用
application（application/）
        │  用例编排；只依赖 domain 的接口（端口）与聚合
domain（domain/）                        ← 零框架依赖：不 import gin/gorm/redis/config
        │  聚合根、值对象、领域服务、仓储与能力端口、领域错误
infrastructure（infrastructure/）
        实现 domain 端口：gorm 仓储、redis 会话/验证码/风控、bcrypt/手机号三列加密、JWT、雪花ID
pkg/、conf/                              通用件与配置
cmd/main.go                              启动 + bootstrap 装配（依赖注入 Composition Root）
```

| 目录 | 职责 |
|---|---|
| `domain/sysuser` | sys_user 聚合根、Credential/Mobile 值对象、`Repository`/`MobileCrypto`/`PasswordHasher` 端口、标识解析领域服务（§3.3③）、聚合不变式（超管保护/密码策略/锁定） |
| `domain/loginlog` | 登录日志聚合；`Append` 语义即“写事务外”，`FailReason` 固定枚举 |
| `domain/authn` | 会话/令牌/验证码/短信/风控端口（TokenIssuer、SessionStore、CaptchaService、SmsSender、SmsCodeStore、LoginGuard、LockGuard）+ 领域错误 |
| `application` | 用例：`Auth`（登录管线三入口汇一）、`SysUser`（CRUD/解锁/重置）、`LoginLog`；错误→业务码映射 |
| `api/v1` + `routes` + `middleware` | 绑定请求 → 调用用例 → 统一响应 `{status,msg,data}`；JWT+Redis 会话鉴权中间件 |
| `infrastructure/db` | GORM 连接、内嵌 SQL 迁移、PO↔领域实体互转、超管 seed（经仓储端口写入） |
| `infrastructure/cache` | Redis 实现：会话（8h 滑动/24h 硬上限/秒踢）、图形码、短信码、失败计数/限流 |
| `infrastructure/crypto` `token` `id` `captcha` | bcrypt+旧算法透明升级、手机 HMAC/AES-GCM 三列、JWT HS256、雪花 ID |
| `cmd/main.go` | run() 优雅关停 + assemble() 装配（Composition Root） |

## 启动

```bash
mysql -uroot -p -e "CREATE DATABASE IF NOT EXISTS \`dugu-hr-saas-admin\` DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"
make run
```

首次启动自动迁移建表并 seed 超管 `admin`（密码见 `bootstrapAdmin`/`SAAS_BOOTSTRAP_ADMIN_PASSWORD`）：

```bash
curl -X POST localhost:8080/api/v1/admin/auth/login/password \
  -H 'Content-Type: application/json' \
  -d '{"identifier":"admin","password":"HrSaas@2026#Admin"}'
```

## 环境变量

| 变量 | 说明 |
|---|---|
| `SAAS_MYSQL_DEFAULT_HOST/PORT/NAME/USER/PASSWORD` | MySQL（dbName 固定 `dugu-hr-saas-admin`） |
| `SAAS_REDIS_HOST/PORT/USERNAME/PASSWORD/DB` | Redis |
| `SAAS_JWT_SECRET` / `SAAS_MOBILE_HMAC` / `SAAS_MOBILE_AES` | 会话签名 / 手机号 HMAC / AES-256 键（64 位 hex）；prod 环境必须覆盖 |
| `SAAS_BOOTSTRAP_ADMIN_PASSWORD/MOBILE/NAME/EMAIL` | 初始超管 |
| `SAAS_ADDR` / `SAAS_DEBUG` | 监听地址 / 调试（GORM SQL 日志） |

## 接口清单（docs §9）

```
POST /api/v1/admin/auth/captcha             获取图形验证码
POST /api/v1/admin/auth/sms/send            发送登录短信（需图形码）
POST /api/v1/admin/auth/login/password      账号或手机 + 密码
POST /api/v1/admin/auth/login/sms           手机 + 短信码
POST /api/v1/admin/auth/logout              登出（Bearer）
GET  /api/v1/admin/auth/me                  当前用户（Bearer）
GET    /api/v1/admin/sys-users              列表（手机返回 mask）
POST   /api/v1/admin/sys-users              创建
PUT    /api/v1/admin/sys-users/{id}         修改
DELETE /api/v1/admin/sys-users/{id}         停用（软删）
POST   /api/v1/admin/sys-users/{id}/unlock  解锁
POST   /api/v1/admin/sys-users/{id}/reset-password   重置密码（强审计）
GET    /api/v1/admin/login-logs             登录日志
GET    /healthz                             存活探针
```

鉴权策略：`middleware.AuthGuard` **全局统一过滤**——白名单外的所有请求必须携带
有效会话（安全默认 deny，新增接口不配置即被拦）。白名单显式列举未登录接口：
`/healthz`、`auth/captcha`、`auth/sms/send`、`auth/login/*`；
`logout/me` 及全部管理面接口仍需登录（见 `routes.PublicPaths`）。

## 响应合同（语言中立约定，给 Java/前端对接）

所有接口 HTTP 恒 200，业务语义在 body：`{status, msg, data}`。

| 项 | 约定 | 原因 |
|---|---|---|
| 雪花 ID | **字符串**输出（`"id":"2118618..."`） | int64 超过 JS `2^53`；Go/Java 的 JSON 库对大整型行为不统一 |
| 时间 | `"yyyy-MM-dd HH:mm:ss"` 字符串 | 不经手 Go `time.Time` / Java `Instant` 的默认序列化差异 |
| 手机号 | mask（`138****0000`），永无明文/密文 | 设计文档 §3.3① |
| 枚举 | 大写字符串（`PASSWORD/SMS/USERNAME/Locked...`） | 不暴露语言整型枚举 |
| 数值 | 除 ID 外均为 ≤2^53 的原生数字（expires_in、remaining_seconds 等） | 前端 JS 安全 |
| 校验失败（400） | `data` 为**非法字段名数组**（camelCase），msg 固定“参数错误” | 不泄漏 Go 结构名/校验 tag/实现细节 |
| 404/405 兜底 | `data` 回显 `{method, path}` | 一次定位方法错/路径错 |
| token | 标准紧凑 JWS（HS256），载荷仅 `uid/jti` | 任何语言的 JWT 库可直接解析 |

> `data` 按接口语义各异（对象/字符串/数组），字段名稳定 camelCase；
> Java 端建议先判 `status`，再按接口契约取 `data`。

## DDD 关键约定

- **domain 零依赖**：聚合/端口无 gin、gorm、redis、config 依赖；领域不读全局时钟，
  时间由接口层传入用例（`now time.Time`），保证可测。
- **端口即领域需求**：应用层只见 `sysuser.Repository`、`authn.SessionStore` 等接口，
  换 MySQL/Redis 实现无需触碰业务逻辑。
- **DB 兜底**：失败锁定 Redis 加速 + `sys_user.fail_count/locked_until` 双写（docs §6）；
  Redis 故障回落 DB 判定，绝不“故障即放行”。
