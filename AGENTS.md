# AGENTS.md — dugu-hr-saas-admin（Go 后端）

任何 AI 在本仓库开发前，**必须先读 `docs/AI开发规范.md`**（规范性文档：铁律、分层、错误码、SQL、安全、接口契约、自检清单）。本文件只是入口与速查，与规范冲突时以规范为准。

## 必读顺序

1. `docs/AI开发规范.md` —— 规范，最高优先级
2. 与本任务相关的 `docs/` 设计文档（`SaaS管理后台-管理账号(sys_user)设计.md`、`系统管理-菜单·配置·字典设计.md`）
3. 将要改动的现有代码

## 速查（详见规范）

- 分层：`api → application → domain ← infrastructure`；domain / application 禁止 import gin / gorm / infrastructure；端口定义在 domain，实现在 infrastructure
- 表结构变更只经 `config/sql/*.sql`；软删 `deleted_at BIGINT` 参与唯一键；"每 X 最多一个"用生成列 + 唯一索引
- 错误码三处同步：`pkg/code` 定义 → `pkg/code/msg.go` → `pkg/i18n/locales/zh-CN.json` + `en-US.json`，缺一即交付不完整
- 认证相关改动过规范 §3.4 安全铁律逐条自查
- 运行时配置走 `PolicyProvider` 快照，禁止业务代码直查 `sys_config`
- 提交前：`go build ./... && go vet ./...`
- 参考仓 `/Users/Admin1/Dugu/workspace/vue3-antdv-admin` **只读**，禁止跨仓 import
- 前端仓库 `dugu-hr-saas-console-web` 的前端规范在其根目录 `AGENTS.md`，改前端去那边，不混在本仓库
