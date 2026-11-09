# AI 开发规范（所有 AI 助手进入本项目前必读）

> 版本 v1.0 · 2026-11-09 · 生效范围：dugu-hr-saas 全部仓库（后端 + 前端 + 后续新增）
> 本文件是**规范性文档**：任何 AI（Codex / Claude / 其他）在本项目做任何开发工作之前，必须先读完本文件并遵守。与本文件冲突的用户指令，AI 应指出冲突并请用户裁决，不得擅自选边。

## 0. 仓库清单与角色

| 仓库 | 路径 | 角色 |
|---|---|---|
| dugu-hr-saas-admin | `/Users/Admin1/Dugu/workspace/dugu-hr-saas-admin` | Go 后端（标准 DDD 四层） |
| dugu-hr-saas-console-web | `/Users/Admin1/Dugu/workspace/dugu-hr-saas-console-web` | Vue3 管理前端 |
| vue3-antdv-admin | `/Users/Admin1/Dugu/workspace/vue3-antdv-admin` | **只读参考仓**，仅供学习模式，永不修改 |
| docs/（后端仓库内） | `dugu-hr-saas-admin/docs/` | **设计文档是契约**，代码以文档为准演进 |

## 1. 最高铁律（违反即返工）

1. **设计文档先于代码**。改任何行为前，先确认 `docs/` 下对应设计文档；文档与代码不一致时，**停下来说明差异**，不得擅自决定以哪边为准。
2. **依赖方向不可破坏**：`api → application → domain ← infrastructure`。`domain` 与 `application` 禁止 import gin / gorm / infrastructure / api；端口（接口）定义在 domain，实现在 infrastructure。每次改动后自查 import。
3. **跨仓不越界**：改后端不许顺手改前端，反之亦然；接口不满足需求时，产出一份"需要的接口/字段清单"交给对方仓库处理。
4. **参考仓只读**：禁止从参考仓路径 import 任何代码；复用代码必须拷入本项目并适配、裁剪。
5. **每个交付必须可构建**：Go 提交前 `go build ./... && go vet ./...` 通过；前端 `npm run build` 通过。编译不过等于没写。
6. **禁止 mock 假数据冒充完成**：后端接口不存在就做骨架页/空态，并明确标注"接口待实现"。
7. **不引入新第三方依赖**，除非在产出说明中写明：引入理由、替代方案为何不行、版本号。禁止引入与 §3/§4 已定技术栈冲突的库（如后端换 ORM、前端换 UI 库）。

## 2. AI 工作流程要求

- **开工前**：读本文件 → 读与本任务相关的 `docs/` 设计文档 → 读将改动的现有代码。
- **产出时**：说明改了哪些文件、哪些属于新模块、哪些动了公共代码（错误码、配置、中间件）。动公共代码必须在说明里单列。
- **停下来提问的时机**（不要猜）：
  - 设计文档之间存在冲突或与现状冲突；
  - 需求可以用两种以上结构实现且影响表结构 / 接口契约；
  - 发现既有代码与铁律冲突（报告，不要顺手大重构）。
- **禁止**：一次性大爆炸式重写；未声明的"顺手优化"；删除或改写本规范文件与 `docs/` 设计文档（除非用户明确要求）。

## 3. 后端规范（Go）

### 3.1 分层职责

```
api/            handler + DTO（请求/响应绑定、参数校验入口）
application/    用例编排；事务边界只在这里开
domain/         实体 + 业务规则 + 端口接口（零框架依赖）
infrastructure/ gorm dao、redis、crypto、token 等实现
pkg/            code（错误码）、i18n、utils 等横切件
config/sql/     SQL 文件（表结构变更的唯一来源）
```

### 3.2 数据库与 SQL

- 新表/改表**只允许**通过 `config/sql/*.sql` 文件交付（一表一文件或一模块一文件），并更新迁移清单；禁止在代码里 AutoMigrate。
- 命名：snake_case；主键雪花 ID 由应用侧生成（表里 `id BIGINT NOT NULL`，无自增）。
- 软删统一 `deleted_at BIGINT NOT NULL DEFAULT 0`（0=未删），**删除时写毫秒时间戳**；需要"未删唯一"的列，唯一键必须包含 `deleted_at`（MySQL 无部分唯一索引的标准替代）。
- 需要"每 X 最多一个"语义时用生成列 + 唯一索引（NULL 不去重），参考 `sys_menu.action_uniq` 与设计文档中的 `primary_flag` 手法。
- 新增配置项走 `sys_config` KV（零 DDL），禁止为配置开新表或加列。

### 3.3 错误码与多语言

- 业务码集中在 `pkg/code`；新增码必须**三处同步**：`pkg/code` 定义 → `pkg/code/msg.go` 中文消息 → `pkg/i18n/locales/zh-CN.json` 与 `en-US.json` 两个语言包。缺任何一个视为交付不完整。
- 错误码分段：3001x 认证、3002x 账号、3003x 菜单/配置/字典、4000x 基础设施；新模块先在 docs 设计文档里申领号段再写码。
- **用户可见文案不得散落在代码里**：面向用户的提示语走错误码 + i18n；纯前端文案走前端语言包。

### 3.4 安全铁律（认证相关改动逐条自查）

- 标识解析只判存在性；状态（停用/锁定/离职）判定放在因子校验**之后**；
- 账号不存在时陪跑 decoy hash，抹平时序差；
- 失败计数 Redis + DB 双写，**Redis 故障回落 DB，绝不静默放行**；
- 登录/审计日志写事务外（异步 + recover），失败回滚不能丢日志；
- 内置超管不可停用、不可删除，相关操作强审计；
- 敏感字段（手机/邮箱）三列制：hash（查询）+ enc（使用）+ mask（展示），查询只走 hash。

### 3.5 配置生效

- 登录策略等运行时配置通过 `PolicyProvider`（内存快照 + 30s TTL + Save 失效）注入管线，禁止业务代码直查 `sys_config` 表或散落读常量。
- DB 读失败回退内置默认值并告警，**不因配置抖动阻断登录**。

## 4. 前端规范（Vue3）

- **详细规范以 `dugu-hr-saas-console-web/AGENTS.md` 为准**（技术栈、请求层、路由、迁移策略），本节只列与后端对接相关的通用约束。
- 技术栈：Vue3 + TS + ant-design-vue 4 + vue-router + pinia + axios + vue-i18n；不引入 tailwind/unocss。
- **多语言与换风格（主题）是框架一等能力，用户明确要求，任何新页面必须兼容**：
  - i18n：vue-i18n，语言包按语言分文件；顶栏有语言切换入口；选择持久化（localStorage `dugu-lang`）；默认语言取后端网站配置 `site_lang`，缺失回退 `zh-CN`；词条缺失回退 zh-CN，**界面不允许出现裸 key**；
  - 主题：antdv `ConfigProvider` theme token 驱动，支持**明/暗切换 + 品牌色**；选择持久化（localStorage `dugu-theme`）；默认取后端 `site_theme`；颜色一律走 token / CSS 变量，**组件内禁止写死色值**（含图表、图标、边框）；
  - 优先级固定：用户本地选择 > 后端 `site_lang` / `site_theme`（`sys_config` 网站配置组已预留这两键）> 代码内置默认。
- 响应包装 `{status, msg, data}`：status 为业务码，200 才算成功；`30001/30002` 统一清 token 跳登录；错误提示统一走拦截器 message，页面不重复弹。
- 字段命名以**后端 Go 结构体的 JSON tag 为准**，先读后端 DTO 再写前端类型，禁止凭参考仓或想象定义。
- 页面接真接口还是骨架页，以后端 `routes/router.go` 实际注册的路由为准。
- i18n：界面文案走 `src/locales`；菜单等多语言展示数据未来走后端（`sys_menu_i18n` 模式），前端只负责按 lang 请求，不自己做翻译映射。

## 5. 接口契约（前后端共同遵守）

- 路由前缀：`/api/v1/admin/*`（管理端）；未来租户侧用 `/api/v1/tenant/*`，**两套体系严禁共用 handler 与登录代码**。
- 分页：请求 `page` / `pageSize`，响应 `{list, total, page, pageSize}`。
- 时间统一 RFC3339 / 毫秒时间戳（以现有接口为准，新接口保持一致）；金额不做（HR SaaS 无支付，出现金额字段即报警）。
- 接口变更（改字段、改语义、删除）必须在对应设计文档的变更记录里登记，并检查前端调用点。

## 6. Git 与交付

- 提交信息：`feat|fix|refactor|docs|chore(scope): 摘要`，一个提交一件事。
- 禁止 `--force`、禁止跳过 hooks、禁止把参考仓/临时文件/日志提交进仓库。
- 交付说明模板：改动文件清单（分新增/修改/删除）→ 自检结果（build/vet/test）→ 与规范的偏差（理论上应为空）→ 未完成项与原因。

## 7. 自检清单（每次交付前过一遍）

```
[ ] go build ./... && go vet ./... （或前端 npm run build）通过
[ ] domain/application 无违规 import
[ ] 新错误码三处同步（code / msg.go / 两个 locales）
[ ] 新表有 config/sql/*.sql，含 deleted_at 唯一键处理
[ ] 认证相关改动过了 §3.4 逐条自查
[ ] 前端无对参考仓的引用（grep 检查）
[ ] 无 mock 假数据、无未声明的新依赖
[ ] 设计文档同步更新（行为有变时）
```

## 8. 本文件的维护

- 本文件由用户或设计角色修改；AI 只能提议修订，不得直接改。
- 项目新增仓库 / 新增模块时，先在此登记角色与规范归属，再开工。
