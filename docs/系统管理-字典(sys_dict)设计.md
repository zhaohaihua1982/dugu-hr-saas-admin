# 系统管理-字典（sys_dict）设计

> 版本 v1.2 · 2026-11-10 · 从《系统管理-菜单·配置·字典设计.md》拆出（原文档 §3 / §4 / §5 / §6 / §10 的字典部分）
> 创建：2026-11-10 · 最后修改：2026-11-10
> v1.2 变更：多语言**存储改走统一表 `sys_i18n_text`**（`res_type='dict_item'`、`field='label'`），本文自建的 `sys_dict_item_i18n` 撤销；接口契约（`?lang=` / `labels`）不变。见 `基础设施-数据文案多语言(i18n)设计.md`
> v1.1 变更：字典项 `label` 支持多语言（本期落地），原 v1.0 §7 的"待拍板"项定稿。
> 依赖：`SaaS管理后台-管理账号(sys_user)设计.md`（操作者身份）、`系统管理-操作日志(sys_oper_log)设计.md`（写操作审计）、`基础设施-数据文案多语言(i18n)设计.md`（译文存储）
> 拆分说明：菜单与系统配置留在原文档（其标题同步改为《系统管理-菜单·配置设计》语义，文件名暂不变以免断链）；本文只负责字典。

## 0. 范围与边界

字典 = **运营可维护的值域展示表**。给一个 `type_code`，返回 `[{value, label}]` 供前端下拉渲染；`label` 按请求语言返回。

| 是 | 不是 |
|---|---|
| 值的**展示层**（label 谁维护 → 运营） | **不是值域权威**：后端接受哪些值由 domain 代码常量决定，字典里多一项不会让它被接受 |
| 与 `sys_config` 并列的**运营配置** | 不是配置项：配置是"系统行为参数"，字典是"业务可选值清单" |
| 承载**开放值域**（新增值无需改代码） | 不承载**封闭枚举**：值域即代码枚举时，文案走前端语言包 `xxxOptions` |
| **多语言**（`label` 进 `sys_i18n_text`，与菜单同一机制） | 不是"多套 value"：`value` 永远单值，语言只影响展示 |

分流判据（与 `基础设施-文件存储(FileManager)设计.md` §13.2.2 同一套，那边是首个真实用例）：

| 值域性质 | 文案归谁 | 落点 |
|---|---|---|
| 开放（业务增长时插数据即可） | 运营 | **字典**（本文） |
| 封闭（值域 = 代码里的枚举） | 开发，随代码发版 | 前端语言包 `模块.xxxOptions.<code>` |
| 系统行为参数（限流、TTL、开关） | 运维 / 运营 | `sys_config` |

**与其它文档的边界**：菜单、系统配置 → 《系统管理-菜单·配置·字典设计.md》；"某个字段该不该走字典" → `FileManager 设计` §13.2.2 的判据表；字典写操作的审计 → `sys_oper_log` 设计。

---

## 1. 表设计

### 1.1 为什么是两张业务表（+ 统一翻译表）

类型与项的生命周期、唯一约束都不同：`uk_type(code)` 全局唯一、`uk_item(type_id, value)` 类型内唯一。合成单表要靠 `parent_id` + 生成列做两套唯一性，收益为零、可读性大幅下降。两张业务表都极简（≤9 字段）。

**翻译不进字典自己的表**：`label` 的多语言存 `sys_i18n_text`（`res_type='dict_item'`、`field='label'`），与菜单、将来所有数据文案共用一套机制。为什么不按"每个资源一张子表"做，见 `基础设施-数据文案多语言(i18n)设计.md` §1——结论是资源多、字段少，通用表才不产生 N 份重复的 DAO 与 fallback。

### 1.2 DDL

```sql
-- 字典类型
CREATE TABLE IF NOT EXISTS `sys_dict_type` (
    `id`         BIGINT      NOT NULL COMMENT '雪花ID，应用侧生成',
    `code`       VARCHAR(64) NOT NULL COMMENT '类型编码，全局唯一；前端取值的唯一入口',
    `name`       VARCHAR(64) NOT NULL COMMENT '类型名称，后台列表展示用',
    `status`     TINYINT     NOT NULL DEFAULT 1 COMMENT '1启用 0停用',
    `remark`     VARCHAR(255) NOT NULL DEFAULT '',
    `deleted_at` BIGINT      NOT NULL DEFAULT 0,
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_dict_type` (`code`, `deleted_at`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- 字典项
CREATE TABLE IF NOT EXISTS `sys_dict_item` (
    `id`         BIGINT       NOT NULL COMMENT '雪花ID，应用侧生成',
    `type_id`    BIGINT       NOT NULL COMMENT '所属类型 id',
    `label`      VARCHAR(64)  NOT NULL COMMENT '默认语言显示名（同时是兜底，NOT NULL 保证永不为空）',
    `value`      VARCHAR(64)  NOT NULL COMMENT '取值；业务落库与前端判断都用它，多语言不改变它',
    `sort`       INT          NOT NULL DEFAULT 0,
    `status`     TINYINT      NOT NULL DEFAULT 1 COMMENT '1启用 0停用',
    `remark`     VARCHAR(255) NOT NULL DEFAULT '',
    `deleted_at` BIGINT       NOT NULL DEFAULT 0,
    `created_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_dict_item` (`type_id`, `value`, `deleted_at`),
    KEY `idx_dict_item_type` (`type_id`, `sort`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- 译文不在这里：label 的多语言存 sys_i18n_text（res_type='dict_item', field='label'），
-- 建表见 config/sql/sys_i18n_text.sql。字典不再有 sys_dict_item_i18n 表。
```

> 建表文件按项目惯例单独落 `config/sql/sys_dict.sql`（迁移器一次执行一条语句，故两表可分两文件），**尚未交付**；init 数据文件已存在（§6），执行顺序见那里。译文表 `sys_i18n_text` 由公共建表文件提供，字典模块不重复建。

### 1.3 唯一键的三条约定

| 键 | 约定 | 为什么 |
|---|---|---|
| `uk_dict_type(code, deleted_at)` | code **全局唯一**，不是 `(type_id, code)` | code 是外部取值的唯一入口，重名即歧义；前端无法表达"哪个 type 的 code" |
| `uk_dict_item(type_id, value, deleted_at)` | value **类型内唯一** | 同一类型下两个 `1` 无法区分 |
| `uk_dict_item_i18n` | ~~一项一语言仅一行~~ → 由通用表 `uk_i18n(res_type,res_id,field,lang,deleted_at)` 保证 | 语义不变，只是约束搬到 `sys_i18n_text`；`Save` 必须是 upsert，不能插第二行 |
| `deleted_at` 参与唯一键 | 软删后可重建同 code / 同 value / 同语言译文 | 与全项目其他表同一手法；不加则"删了不能再建" |

**`code` 一旦被前端引用就不该改**（改 code 等于静默打断所有调用方）。要改走"新建 code + 迁移数据 + 保留旧类型停用一个版本"。

### 1.4 字段语义（哪些能改、哪些不能）

| 字段 | 语义 | 可变性 |
|---|---|---|
| `type.code` | 前端唯一入参，如 `import_type` | 慎改，视同接口契约 |
| `type.name` | 后台列表里的类型名 | 随便改（本期单语：只在管理后台展示，不进用户下拉） |
| `item.value` | 业务真正落库的值 | **慎改**：历史数据里存着旧值，改了就查不到；多语言下也**只有这一个值** |
| `item.label` | **默认语言**显示名，同时是回退兜底 | 随便改，运营主场；改它等于改所有未翻译语言的展示 |
| `item.label`（子表） | 某语言的显示名 | 随便改；缺译回退主表 `label` |
| `item.sort` | 升序 | 随便改（不分语言，各语言同一顺序） |

**`value` 与 `label` 的分工是整个模块最关键的一条**：业务判断、落库、接口传参只许用 `value`；`label` 只用于渲染。前端拿 `label` 做 `if` 判断，运营改一个文案就会功能失效——这是字典类模块最经典的事故。多语言只是把"渲染"这一侧再分成多份，`value` 一侧不受任何影响。

---

## 2. 读取语义

| 规则 | 说明 |
|---|---|
| 只返回**启用**项 | `status=0` 默认不下发 |
| 按 `sort` 升序，同 sort 按 id | 稳定顺序，避免前端每次渲染顺序抖动 |
| 返回 `{value, label}` 二元组 | 不含 id（前端用不到），不含 remark；`label` 已按请求语言解析（`?lang=`，缺省用默认语言） |
| `label` 回退链：请求 lang → 主表 `label` | 主表 `label` NOT NULL，因此**永不返回空串**（与菜单 `sys_menu.name` 同一兜底逻辑） |
| 类型不存在 → `30041` | 拼错 code 是调用方 bug，必须暴露 |
| 类型存在但无项 → `200` + `[]` | 空集是合法状态（新建类型还没配项），不是错误 |
| `withDisabled=1` 才带停用项 | 编辑历史数据回显需要（某条记录存着已停用的 value，列表要能显示它）；默认不带 |

**停用 ≠ 删除**：停用是"不再提供新选择"，历史数据仍引用它。因此：
- 停用项**不能**被物理删除，也不该被清理任务扫到；
- 列表/详情接口回显历史 value 时，前端应带 `withDisabled=1` 取全量再按 value 匹配。

---

## 3. 接口清单

全部需登录（白名单外），与菜单/配置一致：**登录即可见，不做权限过滤**（权限点已在菜单动作预置，见 §8）。

```
GET    /api/v1/admin/dict-types?code=&status=&page=1&pageSize=20
POST   /api/v1/admin/dict-types                       body: {code, name, status?, remark?}
PUT    /api/v1/admin/dict-types/:id                   同上（code 允许改，但见 §1.3 警告）
DELETE /api/v1/admin/dict-types/:id                   逻辑删除；有项则 30045

GET    /api/v1/admin/dict-items?type=import_type&lang=en-US&withDisabled=0
GET    /api/v1/admin/dict-items/:id                   详情，含 labels 全集（编辑页回填用）
POST   /api/v1/admin/dict-items                       body: {typeCode, value, label, labels:{"en-US":"Employee Roster"}, sort?, status?, remark?}
PUT    /api/v1/admin/dict-items/:id                   同上；labels 可只传部分语言（只覆盖所传语言）
DELETE /api/v1/admin/dict-items/:id                   逻辑删除
```

`GET /dict-items` 响应形状（前端下拉直接吃）：

```json
{ "status": 0, "msg": "ok", "data": [
  { "value": "employee", "label": "Employee Roster" }
] }
```

详情接口 `/dict-items/:id` 额外返回 `labels: {"zh-CN":"员工花名册","en-US":"Employee Roster"}`，供编辑表单回填——**列表/取项接口不回 labels**，前端不需要知道翻译子表存在。

四处刻意的取舍：

1. **取项用 `type`（code）而不是 `type_id`**：前端手里只有 code，用 id 会逼前端先查类型、多一次往返，且把内部 ID 泄进契约；
2. **`POST/PUT dict-items` 用 `typeCode` 而不是 `type_id`**：同上，且避免前端传错 id 挂到别的类型下；
3. **`?lang=` 缺省 = 默认语言**：前端不传也能正常工作，多语言是增强不是前置条件；
4. **`labels` 只覆盖所传语言**：PUT 只带 `en-US` 时不动 `zh-TW`，与菜单 `names` 的语义一致（整体覆盖"所传"语言，而非"整表替换"）。

---

## 4. 错误码（30040 段）

| code | 含义 |
|---|---|
| 30041 | 字典类型不存在 |
| 30042 | 字典项不存在 |
| 30043 | 字典类型编码（code）已存在 |
| 30044 | 字典项取值（value）在该类型下已存在 |
| 30045 | 字典类型下存在字典项，禁止删除 |
| 30046 | 字典参数不合法（code/value 格式、长度、空值、`typeCode` 不存在、labels 语言未注册等） |

- **30035（语言不被支持）与菜单共用**，不重复申领：`labels` 的 key 必须在 `i18n.Registered()` 内，非法报 30035；
- 30047~30049 为字典段预留（如需要"项被业务引用不可删"等）；
- 按规范 §3.3：每个码必须在 `pkg/code`、`pkg/code/msg.go`、`pkg/i18n/locales/zh-CN.json` + `en-US.json` 四处同步，缺一即交付不完整。

---

## 5. 领域模型（Go 形状）

```go
// domain/dict
type Type struct {
    ID     int64
    Code   string // ^[a-z][a-z0-9_]{1,63}$ 小写蛇形
    Name   string // 后台列表用，本期单语
    Status int8
    Remark string
}

type Item struct {
    ID     int64
    TypeID int64
    Value  string            // 业务落库值，多语言下仍是单值
    Label  string            // 默认语言展示名（兜底，NOT NULL）
    Labels map[string]string // lang → label；仅编辑与回填用，展示不用
    Sort   int
    Status int8
}

// DisplayLabel 单一解析入口，禁止各处自己写 fallback。
// 回退顺序：请求 lang 的译文 → Label（永远非空）。
func (i Item) DisplayLabel(lang i18n.Lang) string

// Validate：code 非空且匹配 ^[a-z][a-z0-9_]{1,63}$；name 非空 ≤64；
// value 非空 ≤64，字符集 [A-Za-z0-9_.:-]（禁空格与斜杠，避免进 URL/作为 key 前缀时出事）；
// label 非空 ≤64；labels 的 key 必须已注册（否则 30035）、译文非空 ≤64、
// 提交语言数 ≤ 已注册语言数（防脏 key 撑爆子表）；sort 默认 0。
func (t Type) Validate() error
func (i Item) Validate() error

// 删除类型前必须先查项数：>0 返回 ErrDictTypeHasItems(30045)，
// 不做级联软删——字典项被删掉后，历史数据里存着的 value 就失去 label，前端只能显示裸码。
// 同理，项软删时其 i18n 子表行随同一事务软删（与菜单主表/子表同事务处理一致）。
func (s *Service) DeleteType(ctx context.Context, id int64) error
```

**不做级联删除**是本模块的第二个关键决策：删类型连带删项，等于把"历史数据里 value → label 的映射"一起删了。宁可拒绝删除，让运营先把项搬走或停用。

---

## 6. 初始化数据（`config/sql/sys_dict_init.sql`）

项目首个 seed SQL，已交付。约定：

| 项 | 做法 | 理由 |
|---|---|---|
| 幂等 | `INSERT IGNORE`，不用 `ON DUPLICATE KEY UPDATE` | 重复执行跳过；**不覆盖运营在后台改过的 label/sort** |
| ID | seed 固定段 **9e15 起**（类型 `9000000000000001`、项 `...11` 起、译文 `...31` 起） | 与雪花 ID（1e17 量级）永不冲突；一看 ID 就知道是 seed，便于排查与回滚 |
| 现役内容 | `import_type` 类型 + `employee`（员工花名册）一项 + 三语言译文（zh-CN / zh-TW / en-US，落 `sys_i18n_text`） | FileManager 导入批次的首个真实用例 |
| 可选段（第 3 段） | `file_biz` / `file_driver` / `file_status` / `import_status` 四类型 18 项，**默认注释** | 若决定"所有下拉统一走字典"，取消注释即可 |

**执行顺序**：`sys_i18n_text.sql`（公共译文表，已交付）→ `sys_dict.sql`（两表建表，待交付）→ `sys_dict_init.sql`（数据）。init 文件头部已注明该依赖。

**为什么 seed 用固定 ID 而非雪花**：可重复执行、可被 git diff 审阅、可在多环境保持一致；雪花 ID 每次执行都不同，等于每次部署都产生一批"看起来是新数据"的行。

**译文的 seed 要显式写 `zh-CN` 行吗？** 要。主表 `label` 已承担默认语言，但显式写一行 `zh-CN` 让"三语齐全"成为可查询的事实（`SELECT count(DISTINCT lang)` 就能查出缺哪门语言），也避免将来默认语言切到 en-US 时，中文译文无处可取。

---

## 7. 多语言（本期落定，存储走统一表）

**结论：字典项 `label` 支持多语言，本期实现**（v1.1 定稿；v1.2 起存储改走 `sys_i18n_text`，不建字典专用子表）。

为什么字典**必须**支持，而不只是"应该"：

| 理由 | 说明 |
|---|---|
| 值域是开放的 | 运营在后台新增 `attendance` 时，前端语言包**不可能预置** `file.bizOptions.attendance`——前端根本不知道这个值存在。字典是唯一能在运行时给出译名的位置 |
| 多语言是一等能力 | 框架层已定为必须能力，字典若单语，就是英文界面里唯一的"中文孤岛" |
| 契约成本随时间上升 | 晚做要给 `GET /dict-items` 加 `?lang=`、详情加 `labels`，属于契约变更，所有调用方回归 |

**存储与公共机制全部在 `基础设施-数据文案多语言(i18n)设计.md`**：本模块只做两件事——注册 `ResDictItem` + 可翻字段 `label`；读写时调用通用 `Repo`。机制细节（回退链、`ParseLang` 归一化、批量 `Get`、`Save` 覆盖语义、删除同事务）不在本文重复，重复就会漂移。

本模块必须遵守的三条不变式：

| 不变式 | 违反后果 |
|---|---|
| **`value` 永远单值**，语言只影响 `label` | 若用"多套 value"实现多语言，同一业务在不同语言下写入不同的值，历史数据按语言分裂，筛选/统计全错 |
| **`sort` / `status` 不分语言** | 各语言下拉顺序、启停状态必须一致；否则中文界面看到 5 项、英文界面看到 3 项 |
| **主表 `label` 必须等于默认语言译文** | 切换默认语言时**不自动改写**历史 label（那会让一批下拉项突然变语言，属数据事故），由运营自行维护 |

**`type.name` 不做多语言**：它只在管理后台的类型列表里出现，不进任何用户下拉，与 `remark` 同类（内部备注）。真要做，只需在 `domain/i18n` 注册 `dict_type` + `name`，不需要建新表——这正是通用表的收益。

**与错误码语言包的分工不变**：`30041` 这类错误文案归开发（`pkg/i18n/locales`）；字典 `label` 归运营（数据库）。两套机制各管一段，不是重复。

**与错误码语言包的分工不变**：`30041` 这类错误文案归开发（`pkg/i18n/locales`）；字典 `label` 归运营（数据库）。两套机制各管一段，不是重复。

---

## 8. 审计与权限

- **审计**：字典的写操作（类型/项的增删改）按 `系统管理-操作日志(sys_oper_log)设计.md` 登记到 `OperMetaRegistry`：`module=dict`、`action=type.create|type.update|type.delete|item.create|item.update|item.delete`；
- **逻辑删除**：字典一律软删，审计只需记 `id`（`SetDetail` 可选，附 `code`/`value` 便于人读）；
- **权限点**：`system:dict:list/add/edit/remove` 已在菜单动作预置清单（原文档 §11.4.1）中，本期接口先不挂鉴权中间件，权限码就位后直接启用；
- **读取不做权限过滤**：与菜单一致，登录即可见全量（字典是公共元数据，无租户内敏感性）。

---

## 9. 缓存策略

**本期不缓存**，直接查库：两表各几十行、`uk` 索引命中 <1ms，加缓存的复杂度（失效、穿透、一致性）远大于收益。

何时需要加：单个 type 的项数 > 200 或该接口 QPS > 500。届时按 `sys_config` 快照同一模式实现（30s TTL + `atomic.Pointer` + 写后立即失效），**禁止在 dao 层塞包级 `map`**——那是内存泄漏与脏读的标准起点。

---

## 10. 明确不做（本期边界）

- 不做租户隔离（字典是平台级公共元数据）；
- 不做树形/级联字典（需要的场景用专用业务表，不把 `parent_id` 塞进字典）；
- 不做"字典项被业务引用则禁止删除"的引用统计（成本高、收益低；改为**停用**即可）；
- 字典**不承担值域校验**：后端接受哪些值永远以 domain 常量为准，字典只管展示；
- 不做跨类型批量取值（`?type=a,b`）——页面多个下拉就发多个请求，先按简单做；
- 不做字典项的颜色/图标/附件等扩展字段（真需要时按业务表设计，不要长成第二个 `sys_config`）；
- 不做 `type.name` 的多语言（只在后台列表出现，不进用户下拉，见 §7 末段）；
- 不做译文缺失告警/后台"待翻译"清单（回退链已保证不出空串；真需要时再按菜单的同款工具做）。

---

## 11. 实施顺序与验收

顺序：`config/sql/sys_i18n_text.sql`（公共译文表，已交付）→ `config/sql/sys_dict.sql`（两表 DDL）→ PO + DAO → `domain/dict`（枚举、校验；注册 `ResDictItem` + `label`）→ `application/dict`（读写走通用 i18n Repo）→ `api/v1/dict` → 错误码 + 语言包 → 写操作登记 `OperMetaRegistry`（若操作日志中间件已上线）→ 前端菜单页。

验收清单：

1. `go build ./...`、`go vet ./...` 通过；
2. type `code` 重复 → `30043`；同 type 下 item `value` 重复 → `30044`；删非空 type → `30045`；不存在 → `30041` / `30042`；非法参数 → `30046`；
3. `GET /dict-items?type=import_type` 返回 `employee` 项，按 `sort` 升序；
4. `withDisabled=1` 带出停用项，默认不带；
5. 软删后同 `code` / 同 `value` / 同语言译文可重建（`deleted_at` 参与三把唯一键生效）；
6. 全部接口在白名单外（未登录 `30002`）；
7. **多语言**：
   - `GET /dict-items?type=import_type&lang=en-US` 返回 `label: "Employee Roster"`；
   - `?lang=zh-TW` 返回 `員工名冊`；不传 `lang` 返回默认语言；
   - 某语言缺译时回退主表 `label`（**不返回空串**）；
   - `labels` 传 `ja-JP`（未注册）→ `30035`；传 `en_us` → 归一化为 `en-US` 后正常保存，且不产生第二行；
   - `PUT /dict-items/:id` 只传 `en-US` 时 `zh-TW` 译文不变；
   - `GET /dict-items` 响应**不含** `labels`，`GET /dict-items/:id` 才含；
8. 字典写操作在 `sys_oper_log` 有记录（`module=dict`）。

---

## 12. 变更记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-11-10 | 从《系统管理-菜单·配置·字典设计.md》拆出独立成篇：补齐定位与分流判据、读取语义（停用/`withDisabled`/空集非错误）、`value` 与 `label` 分工、唯一键三条约定、字段可变性、接口取舍（`type` 用 code）、错误码 30040 段与预留、领域模型与"不做级联删除"、seed 约定、多语言同构方案与决策点、审计权限、缓存时机、边界与验收 |
| v1.1 | 2026-11-10 | **多语言落定为本期实现**（原 §7 决策点取消）：新增 `sys_dict_item_i18n` 表与 DDL、§1.1 补"为什么必须子表"、接口加 `?lang=` / `labels` / 详情接口、错误码复用 30035、领域模型加 `Labels` + `DisplayLabel`、§7 重写为机制＋三条不变式＋`type.name` 不翻的理由、§6 seed 增加三语译文（`...31` 起）、§11 验收补 7 条多语言用例；`config/sql/sys_dict_init.sql` 同步加译文段与头部说明 |
| v1.2 | 2026-11-10 | **译文存储并入统一表**：撤销自建 `sys_dict_item_i18n`（不建、不迁，字典尚未实现），改存 `sys_i18n_text`（`res_type='dict_item'`、`field='label'`）；§1.1 改写、§1.2 DDL 删子表、§1.3 唯一键改指通用 `uk_i18n`、§7 只留"为什么字典必须多语言"与三条不变式（机制统一由 `基础设施-数据文案多语言(i18n)设计.md` 持有）、§6/§11 执行顺序补 `sys_i18n_text.sql` 在前。**接口契约（`?lang=` / `labels` / 只覆盖所传语言）不变**，前端零改动 |
