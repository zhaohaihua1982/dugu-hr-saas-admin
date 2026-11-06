# dugu-hr-saas-admin · SaaS 管理后台（Gin 版）

SaaS 平台运营方自己的管理后台账号体系（`sys_user`），业务设计见
[docs/SaaS管理后台-管理账号(sys_user)设计.md](docs/SaaS管理后台-管理账号(sys_user)设计.md)。
范围：**P0~P1.5**（密码登录/短信登录/图形验证码/失败锁定/会话/账号管理/登录日志），
租户侧 `hr_account` 留待后续（§12）。

## 技术栈与结构（标准框架：Gin + GORM + Redis + Viper）

```
cmd/main.go                 启动入口（loading：配置→MySQL（迁移+seed 超管）→Redis）
routes/router.go            路由注册（/api/v1/admin/*）
api/v1/                     Handler 层（绑定请求 → service → 统一响应）
service/                    业务层（登录管线、账号管理、错误→业务码映射）
types/                      请求/响应 DTO（binding 校验）
middleware/                 CORS、会话鉴权（JWT + Redis 会话键）
consts/                     常量（枚举/阈值/Redis key）
repository/db/model/        表对象（与 config/sql/*.sql DDL 对应）
repository/db/dao/          仓储（迁移、seed、账号/日志读写）
repository/cache/           Redis（会话/验证码/短信码/失败计数/限流）
pkg/e/                      业务错误码与文案
pkg/utils/                  encryption（bcrypt/手机号三列）、jwt、captcha(SVG)、math、ctl、log、uid(雪花)
config/                     配置加载 + locales/config.yaml + sql/*.sql（内嵌迁移）
docs/                       业务设计文档
```

## 快速启动

```bash
# 1. 建库（若未建）
mysql -uroot -p -e "CREATE DATABASE IF NOT EXISTS \`dugu-hr-saas-admin\` DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"

# 2. 启动（迁移与超管 seed 自动执行）
make run

# 3. 首次登录（P0 验收标准）
curl -X POST localhost:8080/api/v1/admin/auth/login/password \
  -H 'Content-Type: application/json' \
  -d '{"identifier":"admin","password":"HrSaas@2026#Admin"}'
```

首次启动且 `sys_user` 表为空时，会按 `config/locales/config.yaml` 的
`bootstrapAdmin` 创建内置超管 `admin`（建议用环境变量覆盖密码/手机）：

```bash
SAAS_BOOTSTRAP_ADMIN_PASSWORD='YourStr0ng#Pwd' \
SAAS_MYSQL_DEFAULT_PASSWORD='your-mysql-pwd' \
SAAS_JWT_SECRET='<≥16位随机串>' \
SAAS_MOBILE_HMAC='<≥16位随机串>' \
SAAS_MOBILE_AES='<64位hex>' \
make run
```

> `appEnv=prod` 时必须全部使用环境变量覆盖，禁止内置默认密钥上线。

## 环境变量覆盖

| 变量 | 说明 |
|---|---|
| `SAAS_MYSQL_DEFAULT_HOST/PORT/NAME/USER/PASSWORD` | MySQL 连接（dbName 固定 `dugu-hr-saas-admin`） |
| `SAAS_REDIS_HOST/PORT/USERNAME/PASSWORD/DB` | Redis |
| `SAAS_JWT_SECRET` | 会话 JWT 签名密钥 |
| `SAAS_MOBILE_HMAC` | 手机号 HMAC-SHA256 密钥（mobile_hash） |
| `SAAS_MOBILE_AES` | 手机号 AES-256-GCM 密钥（64 位 hex，mobile_enc） |
| `SAAS_BOOTSTRAP_ADMIN_PASSWORD/MOBILE/NAME/EMAIL` | 初始超管 |
| `SAAS_HTTP_PORT` | 监听端口（默认 :8080） |

## 接口清单（docs §9）

```
POST /api/v1/admin/auth/captcha             获取图形验证码
POST /api/v1/admin/auth/sms/send            发送登录短信（需图形码）
POST /api/v1/admin/auth/login/password      账号或手机 + 密码
POST /api/v1/admin/auth/login/sms           手机 + 短信码
POST /api/v1/admin/auth/logout              登出当前会话（Bearer）
GET  /api/v1/admin/auth/me                  当前用户信息（Bearer）

GET    /api/v1/admin/sys-users              列表（手机返回 mask）
POST   /api/v1/admin/sys-users              创建
PUT    /api/v1/admin/sys-users/{id}         修改
DELETE /api/v1/admin/sys-users/{id}         停用（软删）
POST   /api/v1/admin/sys-users/{id}/unlock  解锁
POST   /api/v1/admin/sys-users/{id}/reset-password   重置密码（强审计）
GET    /api/v1/admin/login-logs             登录日志查询
GET    /healthz                             存活探针
```

鉴权方式：登录响应的 `token` 以 `Authorization: Bearer <token>` 携带。
会话 8 小时滑动（每次请求续期），硬上限 24 小时；改密/停用即全端秒踢。

## 短信通道

`service/logSmsSender` 为开发桩（验证码只打日志，不真实外发）。
接入真实通道：实现 `service.SmsSender` 的 `Send` 并替换 `smsSender` 变量即可。
限流与防轰炸规则（1 条/分钟、10 条/天、IP 100 条/天、图形码前置、防枚举 SEND_SKIPPED）已内建。

## 常用 Makefile

```bash
make build   # 编译到 ./main
make run     # 编译并运行
make test    # go test ./...
```
