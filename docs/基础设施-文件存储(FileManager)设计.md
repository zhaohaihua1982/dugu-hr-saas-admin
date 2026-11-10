# 基础设施-文件存储（FileManager）设计

> 版本 v1.2 · 范围：**统一文件存取能力**（本地 / 阿里云 OSS / MinIO 三 driver，按配置切换）
> 创建：2026-11-10 · 最后修改：2026-11-10
> 需求来源：① 支持按配置自动切换 存 本地/oss/minio；② 负责读取存储文件数据，可返回完整链接、path；③ 提供可删除的能力；④ Excel 上传后校验不通过要有标识（导入场景）
> v1.1 变更：新增删除设计（§9）——软删/恢复/彻底删三层语义、逻辑 key 与物理 key 双层、引用计数保护、批量删除、失败补偿、签名 URL 撤销窗口；修正 v1.0 的下载路由前缀（`files/*key` 与 `files/:id` 在 gin 中冲突，改为 `files/content/*key`，白名单边界同时收窄）。
> v1.2 变更：新增 §13 导入场景——内容校验失败的标识落在 `sys_import_batch`（不进 `sys_file`），校验期间 IncRef 保护文件，三种失败处置策略；后续章节号顺延。

---

## 1. 一句话结论与边界

**FileManager 是"存储能力层"，不是业务模块。** 它回答四个问题：文件写到哪儿、怎么读回来、对外的 path 与 URL 分别是什么、删的时候删到什么程度。它**不知道**文件属于谁、属于哪个业务单据——那是业务表和 `sys_file` 的事（§12）。

对外的能力对应四条诉求：

| 诉求 | API | 说明 |
|---|---|---|
| 读取文件数据 | `Get(ctx, key) (io.ReadCloser, *Object, error)` | 流式返回，不整块进内存 |
| 返回完整链接 | `URL(ctx, key) (string, error)` | 带时效的签名链接（默认私有读，见 §2.2） |
| 返回 path | `Object.Key` / 响应字段 `path` | 持久化标识，**入库只存它** |
| 删除 | `SoftDelete` / `Restore` / `Purge` 三层 | 默认进回收站，彻底删需二次确认（§9） |
| 校验失败标识 | `sys_import_batch.status / error_detail` | **不在 FileManager 内**：内容校验结果挂在导入批次上（§13） |

边界（明确不做，见 §15）：不做分片/断点续传、不做图片处理与缩略图、不做 CDN 刷新、不做"切换 driver 时自动搬历史文件"、不做对象版本控制。

---

## 2. 五个决定形状的决策

### 2.1 库里只存 key（path），绝不存 URL

这是整个设计最重要的一条。假设 `sys_config.site_logo` 昨天存的是
`https://dugu-hr.oss-cn-hangzhou.aliyuncs.com/logo/2026/a.png`，今天把 driver 从 oss 换成 minio，会发生什么？

| 存 URL 的后果 | 存 key 的后果 |
|---|---|
| 历史 URL 全部指向旧存储，换 driver 后 404 | key 不变，新 driver 下重新拼 URL 即可 |
| OSS 私有桶签名 URL 15 分钟过期，存进库就是死链 | 每次返回时现签，永不过期 |
| 换 CDN 域名要全表 UPDATE，改漏一条就是脏数据 | 域名是配置，改配置即生效 |
| 本地开发存 `http://127.0.0.1:8080/...`，上线后全废 | 本地与线上 key 同构，数据可直接搬 |

**结论：URL 是派生值，key 是事实。** 任何表、任何配置项、任何缓存里，落盘的都只能是 key。

由此引出一处**与现有文档的冲突**，需同步修订：`docs/系统管理-菜单·配置·字典设计.md` §2.2 里 `site_logo` / `site_bg` 标注为"URL"，按本设计应改为"存 key，接口返回时由 FileManager 拼 URL"。这两项是可公开资源，返回时走 `PublicURL()` 而非签名 URL（见 §2.2）。本设计不擅自改那份文档，待确认后一并修订。

### 2.2 三种 driver 的**读取语义**必须一致——这才是"可切换"的真难点

接口一致是廉价的（三个 `Put/Get/Delete` 谁都会写）；**语义一致才是真成本**。若本地是"目录一扫就能访问"、OSS 是"公开读"，切一次 driver 就等于改一次安全模型，HR SaaS 的身份证、合同、工资条立刻裸奔。

所以统一约定：**默认全部私有读，对外一律短期签名 URL。**

| 维度 | local | oss | minio |
|---|---|---|---|
| 数据可见性 | 禁止 `gin.Static` 直挂上传目录 | 桶 ACL 必须 private | 桶策略必须 private |
| URL 形态 | `{appBaseURL}/api/v1/admin/files/content/{key}?e=&s=` | SDK `SignURL(GET, 15m)` | SDK `PresignedGetObject(15m)` |
| 谁响应字节流 | 应用（`http.ServeContent`，支持 Range） | OSS 直连（不占应用带宽） | MinIO 直连 |
| 公开资源 | `PublicURL()` → `/public/site/{key}`（仅 `site/` 前缀） | `PublicURL()` → CDN 域名 | `PublicURL()` → `endpoint/bucket` |

local 的"签名"由应用自己实现（HMAC，§7），效果与 OSS Presign 等价：**签名即授权，不依赖 Authorization 头**。

这条顺带解决一个前端硬问题：`<img src>`、`<a download>`、新标签打开**发不出 Authorization 头**，走 header 鉴权的接口在图片上根本用不了。签名 URL 让 `<img>` 能用，且不牺牲私有性。

### 2.3 存储配置放 yaml + 环境变量，不放 `sys_config`

规范 §3.2 要求"新增配置项走 `sys_config` KV"，此处**明确例外**，理由三条：

1. **含密钥**：`accessKeySecret` 进 DB，等于把云账号凭证暴露给所有能读 `sys_config` 的路径（含未来的配置导出、备份）；
2. **启动期依赖**：driver 客户端要在进程启动时建立，`sys_config` 依赖 DB，而 DB 连不上时文件服务也不该跟着炸；
3. **改动性质不同**：换 driver 是**运维动作**（涉及数据迁移、网络连通性），不是运营在后台点两下就该生效的东西。热切换会留下"一半旧连接一半新连接"的脏状态。

所以：**配置在 yaml（`config/locales/config.yaml` 新增 `storage` 段），密钥必须支持 `SAAS_*` 环境变量覆盖，切换 driver = 改配置 + 重启。** 与现有 `encryptSecret` 完全同模式。

运营侧仍可配的是**策略类**参数（单文件大小上限、允许扩展名、回收站保留天数），P1 再落 `sys_config`。

### 2.4 切换 driver = 只影响新写入；历史文件的三种处置

必须说清楚，否则上线当天就出事：

```
切 driver 前已存在的文件 → 仍在旧存储，新 driver 读不到
```

三种处置，按场景选：

| 方案 | 做法 | 适用 |
|---|---|---|
| **A. 迁移数据（推荐）** | 提供 `cmd/filemigrate` 小工具：旧 driver 读 → 新 driver 写，key 不变 | 数据量可控、可停机窗口 |
| **B. 只读回源**（P2 可选） | 配置 `storage.readFallback: ["local"]`，读取失败时按序回退旧 driver | 无法停机、历史文件很少被访问 |
| **C. 不迁移** | 直接切，历史文件视为失效（仅限测试环境） | 开发/演练 |

**关键前提：key 与 driver 无关**（不带 bucket、不带域名、不带磁盘路径），方案 A 才能是"纯 copy"，方案 B 才能是"同一个 key 喂给另一个 driver"。这是 §5 key 生成规则要守住的约束。

### 2.5 删除是**状态**，不是动作

最容易做错的地方：把删除写成一个 `DELETE` 语句 + 一次 `os.Remove`。实际后果是——误删无法恢复、审计无痕、签名链接还在飘、对象存储里留一堆没人认领的碎片。

本设计的立场：

- **删除的可逆性由状态机表达**（正常 → 回收站 → 墓碑），不由代码分支表达；
- **逻辑 key 永不变**（业务引用稳定），物理 key 可变（用于让旧 URL 立刻失效）；
- **物理删除必须有墓碑**：`sys_file` 行不删，记 `purged_at`，否则审计日志里的 `file_id` 指向一个不存在的行，等于无法追责；
- **"删"与"清"分开**：用户点删除 = 进回收站；到期清理/合规清除 = 彻底删。两件事的权限、审计、失败处理完全不同。

---

## 3. 分层与目录

遵守规范铁律 2（端口在 domain，实现在 infrastructure）：

```
domain/file/            ← 端口与值对象，零框架依赖（不 import gin/gorm/任何 SDK）
├── ports.go            Store 接口、Object、PutInput、Option
├── key.go              key 生成与校验（NewKey / ValidateKey / TrashKey）
└── errors.go           ErrNotFound / ErrInvalidKey / ErrTooLarge / ErrTypeNotAllowed /
                        ErrRefInUse / ErrNotInTrash / ErrAlreadyPurged

infrastructure/storage/ ← 三个 driver + 装配
├── manager.go          按配置选择 driver，暴露 domain/file.Store
├── local/local.go      os + http.ServeContent
├── oss/oss.go          阿里云 OSS SDK
├── minio/minio.go      MinIO SDK
└── signer.go           HMAC 签名（local 下载与 URL 签发共用）

application/file/       ← 业务侧统一入口（FileService）：编排 sys_file + Store
├── service.go          Upload / Get / URL / SoftDelete / Restore / Purge / BatchDelete
└── repo.go             端口：sys_file 读写（dao 实现）

api/v1/file.go          handler：上传 / 下载 / 签发 URL / 删除 / 恢复 / 列表
```

**两层职责必须分清，这是删除能设计干净的前提：**

| 层 | 职责 | 不知道什么 |
|---|---|---|
| `domain/file.Store`（存储端口） | 对象的字节流与元信息：Put / Get / Stat / Delete / Move / URL | 不知道回收站、不知道谁传的、不知道引用计数 |
| `application/file.Service`（业务入口） | 状态机、引用计数、审计、批量、失败补偿 | 不知道是本地磁盘还是 OSS |

`Store.Delete` 就是**物理删除对象本身**，没有"软删"概念。软删是 `sys_file.deleted_at` 的业务状态 + 对象位置（`trash/` 前缀）的组合。把软删塞进 Store 接口，会让三个 driver 都要各自实现一遍状态机——这是典型的层次错位。

---

## 4. 端口定义（domain/file/ports.go）

```go
package file

import (
	"context"
	"io"
	"time"
)

// Store 对象存储端口。三个 driver 的语义契约见设计文档 §2.2。
type Store interface {
	// Put 写入对象，返回落库用的 key。实现必须保证：key 已存在时报错，绝不静默覆盖。
	Put(ctx context.Context, in PutInput) (*Object, error)

	// Get 读取对象字节流。调用方必须 Close。
	// 返回的 ReadCloser 可能同时实现 io.Seeker（local 完整支持，用于 http.ServeContent 的 Range）。
	Get(ctx context.Context, key string) (io.ReadCloser, *Object, error)

	// Stat 只取元信息，不传数据。用于存在性校验、删除前预检、前端预检。
	Stat(ctx context.Context, key string) (*Object, error)

	// Delete 删除对象。key 不存在视为成功（幂等）。
	Delete(ctx context.Context, key string) error

	// Move 移动对象（src 不存在返回 ErrNotFound，dst 已存在返回错误）。
	// 语义：本地 os.Rename；OSS/MinIO 为 CopyObject + DeleteObject（非原子，
	// 复制成功而删除失败会留双份，重试 Delete 即可，无害）。
	// 用途：软删移入 trash/、恢复移回原位（§9.2）。
	Move(ctx context.Context, srcKey, dstKey string) error

	// URL 签发**带时效**的完整访问链接（默认 15 分钟，可配）。
	URL(ctx context.Context, key string, opt ...URLOption) (string, error)

	// PublicURL 返回永久公开链接，仅用于确实可公开的资源（站点 logo、背景图）。
	// 私有桶/本地私有目录下调用该方法是错误的，实现应返回 ErrNotPublic。
	PublicURL(key string) (string, error)

	// Driver 返回当前 driver 名（local/oss/minio），用于日志与灰度排查。
	Driver() string
}

// Lister 可选能力：遍历对象（孤儿清理、容量统计用）。
// 不放进 Store 主接口——P0 不需要，避免为三个 driver 增加无谓实现负担
// （用类型断言 `if l, ok := store.(file.Lister); ok`）。
type Lister interface {
	List(ctx context.Context, prefix string, fn func(*Object) error) error
}

// Object 对象的元信息 + 对外视图。
// 注意：Key 是逻辑 key，**绝不是磁盘绝对路径**（禁止泄露服务器目录结构）。
type Object struct {
	Key         string    `json:"path"`          // 如 contract/2026/11/10/1849....pdf
	Size        int64     `json:"size"`
	ContentType string    `json:"contentType"`
	ETag        string    `json:"etag,omitempty"`
	ModTime     time.Time `json:"modTime"`
	URL         string    `json:"url,omitempty"` // 由 URL() 填充，不落库
}

type PutInput struct {
	Biz         Biz       // 业务域白名单，见下
	Filename    string    // 用户原始文件名，仅用于取扩展名与下载时的 Content-Disposition
	Reader      io.Reader
	Size        int64     // 已知长度时传，未知传 -1
	ContentType string    // 可为空，实现须自行嗅探（§10）
}

type Biz string

const (
	BizAvatar   Biz = "avatar"   // 头像
	BizResume   Biz = "resume"   // 简历
	BizContract Biz = "contract" // 合同/证明
	BizImport   Biz = "import"   // 批量导入源文件
	BizExport   Biz = "export"   // 导出结果
	BizSite     Biz = "site"     // 站点 logo/背景等公开资源
	BizTemp     Biz = "temp"     // 临时文件，生命周期由清理任务管
)
```

`Biz` 的存在是为了**收敛 key 空间**：不让调用方随便传前缀，否则一年后存储里会出现 `test/`、`新建文件夹/`、`张三的/` 这类东西。

---

## 5. key 生成规则（domain/file/key.go）

```
{biz}/{yyyy}/{MM}/{dd}/{snowflakeID}-{safeName}{ext}
例：contract/2026/11/10/1849203918234567890-劳动合同-张三.pdf
```

- `{snowflakeID}`：**唯一性保证**，主体。免查重、免加锁，重名永远不可能覆盖。
- `{safeName}`：**可读性后缀**，由用户原始文件名**清洗**而来（规则见下）。unique 由雪花 ID 保证，safeName 重复、缺失都无害。

**safeName 清洗规则**（`domain/file/key.go` 内实现，输入 = 用户原始文件名去扩展名）：

1. 剥离任何路径成分（取 `filepath.Base` 语义，防 `../../`）；
2. 删除文件系统/URL 非法字符：`/ \ : * ? " < > |` 与控制字符、`\x00`；
3. 空白字符（空格/换行/tab）折叠为单个 `-`；连续 `-` 合并；首尾 `-` 去掉；
4. 结果为空（如原文件名全是符号）→ 后缀整体省略；
5. 长度截断到 **60 个 rune**（UTF-8 下中文最多 180 字节，key 总长仍远小于 255 上限）；
6. 扩展名不进 safeName——从原始名提取、小写化、白名单校验后单独拼接。

落盘目录树实例（local driver，basePath = `./data/uploads`）：

```
data/uploads/
├── avatar/2026/11/10/1849203918234567801-头像.png              ← 头像
├── contract/2026/11/10/1849203918234567802-劳动合同-张三.pdf    ← 合同
├── resume/2026/11/09/1849182277654321003-张三简历.pdf           ← 简历
├── import/2026/11/10/1849203918234567804-花名册-1105最新版.xlsx ← 导入源文件
├── export/2026/11/10/1849203918234567805-花名册-导出.xlsx       ← 导出结果
├── site/2026/01/15/1800000000000000001-logo.png                ← 站点 logo（唯一公开前缀）
└── trash/                                                      ← 回收站（软删且 strictRevoke 时）
    ├── avatar/2026/11/10/1849203918234567801-头像.png          ← 原 key 结构整体平移
    └── contract/2026/10/31/1848711111111111111-劳动合同.pdf
```

`sys_file.origin_name` 仍原样保留**未经清洗的完整原始文件名**（含可能被清洗掉的字符），下载时经 `Content-Disposition` 还给用户；key 里的 safeName 只是给存储控制台/目录浏览用的可读副本，两者不要求一致。

四条约束，每条都对应一个真实事故：

| 规则 | 防的事故 |
|---|---|
| **原文件名必须清洗后才进 key（safeName）** | 路径穿越（`../../etc/passwd`）、中文/空格/`?`/`#` 在 URL 与对象存储里的编码地狱、Windows 保留名（`CON`、`NUL`） |
| **雪花 ID 作为文件名主体** | 免查重、免加锁、天然有序（按 key 排序≈按时间排序）；与全项目 ID 生成方式一致；同日同名文件不会互相覆盖 |
| **只保留扩展名，且小写化、白名单校验** | `.PHP`、`.jsp`、`.html`、`.svg` 是上传型 getshell / 存储型 XSS 的经典入口 |
| **按 年/月/日 三级分片** | 本地文件系统单目录超过 ~10 万文件后 `readdir` 显著劣化；OSS 无所谓，但保持三 driver 同构，便于迁移与排查 |

```go
var keyPattern = regexp.MustCompile(`^[a-z]+/\d{4}/\d{2}/\d{2}/[0-9]{1,20}(?:-[^/\\:*?"<>|\x00-\x1f]{1,180})?\.[a-z0-9]{1,8}$`)
//                                                          └─ safeName 后缀可选（历史 key / 清洗后为空时省略）

// ValidateKey 是所有读取/删除入口的第一道闸：拒绝空、拒绝绝对路径、拒绝 .. 与 \\、
// 拒绝 URL 编码后的变体、拒绝不匹配 keyPattern 的字符串。
func ValidateKey(key string) error {
	if key == "" || len(key) > 255 {
		return ErrInvalidKey
	}
	if strings.Contains(key, "..") || strings.ContainsAny(key, "\\\x00") || strings.HasPrefix(key, "/") {
		return ErrInvalidKey
	}
	if !keyPattern.MatchString(key) {
		return ErrInvalidKey
	}
	return nil
}

// NewKey 生成逻辑 key：biz 白名单校验 → 雪花 ID → safeName 清洗 → 扩展名白名单校验。
func NewKey(biz Biz, originName string) (string, error) { /* 见 domain/file/key.go */ }

// TrashKey 逻辑 key → 回收站物理 key（§9.2）。幂等：已是 trash/ 前缀则原样返回。
func TrashKey(logicalKey string) string { return trashPrefix + logicalKey }

const trashPrefix = "trash/"
```

> **`ValidateKey` 必须由 driver 内部再调一次**，不能只靠 API 层调。理由：将来任何内部调用（导出、迁移、清理任务）都可能传入外部来源的 key，防御要放在最底层。`trash/...` 形态的 key 由 `Store.Move` 内部处理，不经过 `ValidateKey`（它是物理 key，不是对外 key）。

---

## 6. 配置（config/locales/config.yaml 新增段）

```yaml
storage:
  # driver: local | oss | minio —— 只校验选中的那个，其余留空不报错
  driver: local
  maxSizeMB: 20            # 单文件上限
  signExpireMinutes: 15    # 签名 URL 有效期
  allowExt: [jpg, jpeg, png, webp, gif, pdf, doc, docx, xls, xlsx, ppt, pptx, csv, txt, zip]

  # ===== 删除相关 =====
  trashRetainDays: 30      # 回收站保留天数，到期由清理任务彻底删
  strictRevoke: false      # true = 软删时立刻把对象移入 trash/，旧签名 URL 立即失效（§9.5）

  local:
    basePath: "./data/uploads"   # 落盘根目录，启动时自动创建
    # 私有读默认走应用下载接口；若前面挂了 nginx/CDN 直出，填这里
    publicBaseURL: ""

  oss:
    endpoint: "oss-cn-hangzhou.aliyuncs.com"
    bucket: ""
    accessKeyId: ""            # SAAS_OSS_ACCESS_KEY_ID
    accessKeySecret: ""        # SAAS_OSS_ACCESS_KEY_SECRET
    cdnDomain: ""              # 仅 PublicURL 使用

  minio:
    endpoint: "127.0.0.1:9000"
    accessKey: ""              # SAAS_MINIO_ACCESS_KEY
    secretKey: ""              # SAAS_MINIO_SECRET_KEY
    bucket: ""
    useSSL: false
    region: "us-east-1"
```

`config/config.go` 需同步：

- `Conf` 增加 `Storage *Storage`；
- `applyEnvOverrides` 增加 `SAAS_STORAGE_DRIVER` / `SAAS_OSS_ACCESS_KEY_ID` / `SAAS_OSS_ACCESS_KEY_SECRET` / `SAAS_MINIO_ACCESS_KEY` / `SAAS_MINIO_SECRET_KEY`；
- `validate` 增加：driver 必须在枚举内；**只校验被选中 driver 的必填项**（部署只用一个，不该被迫填另外两个的密钥）；`maxSizeMB > 0`；`trashRetainDays` 必须在 1~3650；
- **fail fast**：driver 名写错或必填项缺失，启动即 panic，不做降级。

> 与 §3.5「DB 读失败回退默认值」的区别要讲清：那是**运行时**配置抖动，降级是为了不阻断登录；这里是**启动期**配置错误，降级只会把问题推迟到第一个用户上传时才暴露（而且表现为 500），必须当场炸。

`encryptSecret` 建议新增可选键：

```yaml
encryptSecret:
  fileSign: ""   # SAAS_FILE_SIGN_SECRET；留空则由 jwtSecret 派生子密钥
```

派生方式：`signKey = HMAC-SHA256(jwtSecret, "file-sign-v1")`。**不为文件签名单独强加一个必填密钥**（少一个上线踩坑点），但派生子密钥保证"文件签名"与"会话 JWT"不共用同一把 key——同一把 key 用于两种用途，是密码学上的典型误用。

---

## 7. 签名 URL 规范（local driver）

```
GET /api/v1/admin/files/content/{key}?e={expUnixSec}&s={sigBase64URL}

sig = Base64URL( HMAC-SHA256( signKey, key + "\n" + strconv.FormatInt(exp, 10) ) )
```

校验顺序（顺序本身是安全设计）：

1. `ValidateKey(key)` —— 先挡畸形输入，避免签名比对被用于探测；
2. `exp` 存在且 `now <= exp`；
3. `hmac.Equal(计算值, s)` —— **常数时间比较**，禁止 `==`（时序侧信道）；
4. 通过后（**且仅当 `strictRevoke=false` 时**）查一次 `sys_file`：`deleted_at > 0 || purged_at != nil` 一律拒绝——这是 §9.5 的吊销兜底；
5. `Get` + `http.ServeContent`。

配套约束：

- **有效期上限硬编码 24h**，配置值超过则截断——防止有人为了"方便"签出一年期链接；
- **签名只绑定 key + 过期时间**，不绑定用户。理由：文件是资源级授权，不是身份级；绑定用户会导致"分享给同事后对方打不开"，而分享在 HR 场景是常态。要撤销授权就删文件或换 `signKey`（换 key 会让所有链接立刻失效，这是可接受的全局撤销手段）；
- 该下载路由所在的**前缀必须加入 AuthGuard 白名单**，因此这条前缀下**只允许注册签名校验型接口**，不得塞任何其他业务接口。

```go
// routes/router.go
// 安全提示：该前缀进白名单（签名即授权），下面只允许注册 /files/content/* 下载路由，
// 严禁在此前缀注册任何其他业务接口——否则等于全局免登录。
admin.GET("files/content/*key", fileAPI.Download)
```

> **v1.0 的坑修正**：v1.0 把下载放在 `files/*key`、管理接口放 `files/:id`。gin 的路由树中，同层既有通配 `*key` 又有参数 `:id` 会 panic（wildcard conflicts with existing children）。所以下载独立到 `files/content/*key`，同时把白名单前缀从 `files/` 收窄到 `files/content/`——白名单范围小一倍，误配风险同步下降。

---

## 8. 写入与读取流程

### 8.1 上传

```
POST /api/v1/admin/files (multipart)
  → handler 取 file header（大小先按 maxSizeMB 挡，不等读完）
  → 嗅探前 512 字节定 Content-Type（不信任客户端声明）
  → 扩展名白名单 + MIME 白名单双重校验
  → domain.NewKey(biz, originName) 生成 key（内部：雪花 ID + safeName 清洗 + 扩展名白名单）
  → store.Put(ctx, PutInput{...})        // 流式写入，io.Copy 不落内存
  → sys_file 落一行（file_key=storage_key=key, uploader_id, ref_count=0）
  → 返回 {id, path, url, size, contentType}
```

业务只需要把 `path` 存进自己的表（如 `sys_user.avatar`）。

### 8.2 读取（三种场景，接口一个就够）

```go
rc, obj, err := store.Get(ctx, key)
defer rc.Close()
// 场景 1：应用内解析（导入 Excel）→ io.Copy 到临时文件 / excelize 直读
// 场景 2：业务 DTO 带出 → obj.URL, _ = store.URL(ctx, key)，塞进响应
// 场景 3：API 直出 → handler 里 http.ServeContent(w, r, obj.Key, obj.ModTime, seeker)
```

批量场景（列表页 100 条都要图）：**不需要批处理 API**。OSS/MinIO 的 Presign 与本地 HMAC 都是纯本地计算（不发网络请求，微秒级），100 次签名开销可忽略。

---

## 9. 删除设计

### 9.1 三种删除，语义与默认值

| 操作 | 业务含义 | 对象（字节） | `sys_file` 行 | 可否恢复 | 权限 |
|---|---|---|---|---|---|
| **逻辑删除** `SoftDelete` | 用户点"删除" | 不动（`strictRevoke=true` 时移入 `trash/`） | `deleted_at` 写毫秒时间戳 | ✅ 30 天内可 `Restore` | `file:delete` |
| **恢复** `Restore` | 从回收站捞回 | 若已移动则移回原位 | `deleted_at` 归 0 | — | `file:delete` |
| **彻底删除** `Purge` | 合规清除 / 到期清理 | **真删** | 保留墓碑行，写 `purged_at` | ❌ 不可逆 | `file:purge`（本期=超管） |

为什么**软删时默认不动对象字节**：移动/复制是三次网络往返（Stat + Copy + Delete），而收益只是"提前 15 分钟失效一个签名 URL"。默认接受这个窗口（§9.5 详述，S3/MinIO 固有行为也是如此）；对身份证、合同这类高敏数据，打开 `strictRevoke=true` 换取立即失效。

为什么 **Purge 后要留墓碑行**：操作日志（`sys_oper_log`）里记的是 `file_id`。若行被物理删除，日志里的 id 指向不存在的数据，追责链断在这里。留 `purged_at` 的墓碑行，代价是几十字节，收益是审计闭环。

### 9.2 逻辑 key 与物理 key 双层

```
逻辑 key（file_key）：contract/2026/11/10/1849....pdf   ← 永不变，业务引用、签名 URL 都用它
物理 key（storage_key）：同上（正常） / trash/contract/...（软删且 strictRevoke）    ← 可变
```

`Object` / `Store` 层面只认物理 key，`FileService` 负责两者映射。这条设计的价值在 §9.5：**换物理 key = 让旧签名 URL 立即 404**，而业务代码完全不用感知。

恢复时 `storage_key` 移回 `file_key`，若目标已存在（极端并发下重复上传同名？雪花 ID 下不可能）则报错而非覆盖。

### 9.3 引用计数保护

同一个文件可能被多处引用（合同模板被 50 份合同引用、站点 logo 被多套配置引用）。直接删会打断引用方。

规则：

- `ref_count > 0` → `SoftDelete` 拒绝，返回 `ErrRefInUse`（40015）；
- 引用方使用前后必须 `IncRef` / `DecRef`（`FileService` 提供，走 `sys_file.ref_count`，`UPDATE ... SET ref_count = ref_count + 1` 原子自增，不做"读-改-写"）；
- `ref_count = 0` 才允许进回收站；
- `Purge` **不看** `ref_count`（合规清除属最高优先级，先解引用再清，由调用方负责），但必须记日志并在审计 detail 里写下当时的 `ref_count`，便于事后查"这次清除是否误伤"。

### 9.4 流程

**软删**

```
DELETE /api/v1/admin/files/:id
  → 查 sys_file（不存在 / 已 purged → 40010；deleted_at>0 → 幂等成功）
  → ref_count > 0 → 40015
  → strictRevoke ? store.Move(file_key, trash/{file_key}) : 跳过
  → sys_file.deleted_at = now（与 storage_key 同一事务）
  → operlog.SetDetail：记 id + file_key + biz + 大小（逻辑删除按 §9 操作日志约定只记 id）
```

**恢复**

```
POST /api/v1/admin/files/:id/restore
  → 校验 deleted_at > 0 且 purged_at IS NULL，否则 ErrNotInTrash（40016）
  → storage_key != file_key ? store.Move 移回 : 跳过
  → sys_file.deleted_at = 0，storage_key = file_key
```

**彻底删**

```
DELETE /api/v1/admin/files/:id/purge
  → 仅超管（本期；RBAC 上线后改挂 file:purge）
  → **先 operlog.SetDetail(整行 JSON)**（呼应操作日志设计 v1.1：物理删除记整条数据）
  → store.Delete(storage_key)        // 幂等
  → sys_file.purged_at = now         // 行保留
  → 删失败：purge_attempts++，写 next_retry_at，交给补偿任务（§9.6），行保持 deleted_at
```

**批量删除**

```
POST /api/v1/admin/files/batch-delete   { ids: [...], purge: false }
  → 上限 100 条（超出 400），逐条独立执行，不整体回滚
  → 返回 { success: 87, failed: [{id, status, msg}] }
```

部分成功是**正确语义**而非妥协：100 个文件里第 43 个被引用保护拒绝，不该让前 42 个的回滚——那会让用户看到"点了没反应"。前端按 failed 列表逐条提示。

### 9.5 签名 URL 的撤销窗口（必须诚实说明）

**问题**：签名 URL 一旦发出，服务端无法主动召回。用户 10:00 点删除，10:05 那个尚未过期的链接仍可打开。

**三条对策，按 driver 能力分层**：

| 手段 | 生效范围 | 成本 |
|---|---|---|
| 下载前查 `sys_file`（`deleted_at>0 || purged_at!=nil` 即拒） | 仅 local（走应用） | 每下载一次 DB 查询 → 用 Redis 缓存 tombstone，TTL = 24h（= 签名上限） |
| 软删时移入 `trash/`（`strictRevoke=true`） | 三 driver 全适用（URL 里的 key 变了，自然 404） | 一次 Move（本地 rename / 对象存储 Copy+Delete） |
| 彻底删时直接删对象 | 三 driver 全适用 | 无 |

**OSS / MinIO 的 Presign 无法撤销**——这是协议层事实，不是实现缺陷。所以对公有云场景，唯一可靠的"立即失效"就是**别让对象待在原 key 上**（`strictRevoke=true`）或**直接删掉**。文档里必须写清楚，避免运营误以为"点了删除就立刻不可访问"。

P1 落地顺序：local 走 Redis tombstone（`file:revoked:{file_key}`，TTL 24h），OSS/MinIO 建议对 `contract` / `resume` 两类 biz 默认开 `strictRevoke`。

### 9.6 失败与补偿

物理删除可能失败（网络抖动、OSS 限流、本地磁盘只读）。**绝不能让删除失败把业务请求卡死**：

- `Purge` 失败 → `purge_attempts++`、`next_retry_at = now + 2^n 分钟`（n 上限 5），返回失败给用户？
  **不。** 用户点"彻底删除"必须看到真实结果 —— 失败就返回 40014 并提示稍后重试，但行已标记为待清理，由补偿任务兜底。**给用户假成功是最坏的选择**；
- 补偿任务（P1，与登录日志清理同处一个定时调度）：扫 `deleted_at > 0 AND purged_at IS NULL AND next_retry_at <= now`，重试；`purge_attempts >= 5` → 告警（日志 error 级别 + 监控打点），暂停重试，等人工介入；
- 恢复时 `Move` 失败 → 事务回滚，`deleted_at` 不清零，用户重试即可（状态一致，无中间态）。

### 9.7 到期清理

`trashRetainDays`（默认 30）后自动 `Purge`。清理任务本身也是"删除操作"，必须：

- 以系统身份执行（`operator_id = 0`，`operator_name = "system"`），与人工操作可区分；
- 同样留墓碑行、同样记操作日志（`module=file, action=auto-purge`）；
- 单批次限量（如 500 条/轮），避免一次扫全表拖垮 DB。

### 9.8 审计与权限

呼应 `docs/系统管理-操作日志(sys_oper_log)设计.md` v1.1 的三条既定规则，本模块严格对齐：

| 操作 | OperMetaRegistry 登记 | detail 内容 |
|---|---|---|
| 逻辑删除 | `DELETE /files/:id` → `file/soft-delete` | `{id}`（按约定逻辑删除只记 id） |
| 批量删除 | `POST /files/batch-delete` → `file/batch-delete` | `{ids: [...], purge: false}` |
| 恢复 | `POST /files/:id/restore` → `file/restore` | `{id}` |
| 彻底删除 | `DELETE /files/:id/purge` → `file/purge`，**`PhysDelete: true`** | **整行 JSON**（`SetDetail` 必须在 `store.Delete` 之前调用，删完就查不到了） |

权限：本期系统尚无 RBAC，`Purge` 收紧到超管（与"内置超管不可停用"同一哲学：不可逆操作只给最高权限）；`SoftDelete` / `Restore` 所有已登录管理员可用。RBAC 上线后分别改挂 `sys_menu.action_code = file:delete` / `file:purge`。

### 9.9 接口清单

| 方法 | 路径 | 用途 | 鉴权 |
|---|---|---|---|
| POST | `/api/v1/admin/files` | 上传 | header |
| GET | `/api/v1/admin/files/content/*key` | 下载/预览（签名） | **白名单 + 签名** |
| GET | `/api/v1/admin/files` | 列表（支持 `biz`/`deleted`/分页） | header |
| GET | `/api/v1/admin/files/:id/url` | 签发/刷新签名 URL | header |
| DELETE | `/api/v1/admin/files/:id` | 逻辑删除 | header |
| POST | `/api/v1/admin/files/:id/restore` | 恢复 | header |
| DELETE | `/api/v1/admin/files/:id/purge` | 彻底删除 | header + 超管 |
| POST | `/api/v1/admin/files/batch-delete` | 批量（上限 100） | header |

---

## 10. 安全清单（逐条自查）

| 风险 | 处置 |
|---|---|
| 路径穿越 | `ValidateKey` 白名单正则，driver 内二次校验；本地 driver 拼接后用 `filepath.Clean` 再校验前缀仍在 `basePath` 内（trash/ 同样校验） |
| 存储型 XSS | 上传侧拒 `html/htm/svg/js/xml`；下载侧响应头 `X-Content-Type-Options: nosniff`，非 `image/*` 与 `application/pdf` 一律 `Content-Disposition: attachment`，并加 `Content-Security-Policy: default-src 'none'` |
| 伪装扩展名 | 内容嗅探 `http.DetectContentType` 与扩展名比对，不一致拒绝（`.jpg` 里装 PHP 的老套路） |
| 目录遍历列举 | 本地 driver 绝不注册 `gin.Static`；所有访问必须经 key |
| 目录结构泄露 | 响应里只给逻辑 key，绝不返回磁盘绝对路径（含错误信息里） |
| 大文件打爆内存 | `Put`/`Get` 全流式；`maxSizeMB` 在 `MultipartReader` 层前置拦截 |
| 覆盖攻击 | key 含雪花 ID，`Put` 遇已存在直接报错；`Move` 遇目标已存在同样报错 |
| 链接长期外泄 | 签名默认 15 分钟，上限 24h |
| **删除后链接仍可访问** | §9.5：Redis tombstone（local）+ `strictRevoke`（公有云）+ 签名 15 分钟窗口的显式披露 |
| 云凭证泄露 | 密钥只走环境变量；日志里打印配置时对 secret 打码（`ak***yz`） |
| SSRF | 本组件不接受外部 URL 作为输入（不做"远程抓取"），无需处理 |

---

## 11. 错误码申领（4001x 文件存储段）

规范 §3.3 要求新模块先申领号段。按现有 40001 数据库段之后取 **40010~40019**：

| 码 | 常量 | 场景 |
|---|---|---|
| 40010 | `ErrorFileNotFound` | 对象/记录不存在；key 非法也归此码（对外统一文案，不区分，避免探测） |
| 40011 | `ErrorFileTooLarge` | 超过 `maxSizeMB` |
| 40012 | `ErrorFileTypeNotAllowed` | 扩展名或嗅探类型不在白名单 |
| 40013 | `ErrorFileSignInvalid` | 签名缺失/过期/不匹配 |
| 40014 | `ErrorStorageUnavailable` | driver 报错（网络、鉴权、磁盘满）——**对外只给通用文案，细节进日志** |
| 40015 | `ErrorFileRefInUse` | 文件仍被业务引用，禁止删除 |
| 40016 | `ErrorFileNotInTrash` | 恢复失败：不在回收站或已彻底删除 |
| 40017 | `ErrorFilePurgeDenied` | 无彻底删除权限（本期：非超管） |

按规范三处同步：`pkg/code/code.go` → `pkg/code/msg.go` → `pkg/i18n/locales/zh-CN.json` + `en-US.json`。

domain 哨兵映射（写在 `application.CodeOf`）：`ErrNotFound`/`ErrInvalidKey`→40010、`ErrTooLarge`→40011、`ErrTypeNotAllowed`→40012、`ErrRefInUse`→40015、`ErrNotInTrash`/`ErrAlreadyPurged`→40016、其他→40014。

---

## 12. `sys_file` 元数据表

删除设计落地的前提是这张表——`FileManager` 的"状态"全在这里。P0 建表（不做表就做不了软删）。

```sql
CREATE TABLE sys_file (
  id             BIGINT       NOT NULL COMMENT '雪花ID',
  file_key       VARCHAR(255) NOT NULL COMMENT '逻辑key：业务引用与签名URL使用，永不变',
  storage_key    VARCHAR(255) NOT NULL COMMENT '物理key：软删移入 trash/ 时变化',
  biz            VARCHAR(32)  NOT NULL COMMENT '业务域 avatar/resume/contract/...',
  origin_name    VARCHAR(255) NOT NULL COMMENT '原始文件名（下载时 Content-Disposition 用）',
  size           BIGINT       NOT NULL DEFAULT 0,
  content_type   VARCHAR(128) NOT NULL DEFAULT '',
  driver         VARCHAR(16)  NOT NULL COMMENT '写入时的 driver，供回源/迁移判断',
  uploader_id    BIGINT       NOT NULL DEFAULT 0 COMMENT '0=系统',
  ref_count      INT          NOT NULL DEFAULT 0 COMMENT '业务引用计数，>0 禁删',
  created_at     DATETIME(3)  NOT NULL,
  deleted_at     BIGINT       NOT NULL DEFAULT 0 COMMENT '0=正常；>0=回收站，值为毫秒时间戳',
  purged_at      DATETIME(3)  NULL DEFAULT NULL COMMENT '物理删除时间；非空=墓碑行，不可恢复',
  purge_attempts TINYINT      NOT NULL DEFAULT 0,
  next_retry_at  DATETIME(3)  NULL DEFAULT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_file_key (file_key, deleted_at),
  KEY idx_biz_created (biz, created_at),
  KEY idx_purge_scan (deleted_at, purged_at, next_retry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='文件元数据（墓碑行保留以便审计）';
```

三个设计点：

1. **`deleted_at` 用 BIGINT 毫秒**，与全项目软删约定一致（规范 §3.2）；`uk_file_key (file_key, deleted_at)` 是"未删唯一"的标准写法；
2. **`purged_at` 与 `deleted_at` 分开**：前者是"字节没了"，后者是"用户删了"。混用一个字段就分不清"在回收站"和"已彻底清除"；
3. **`idx_purge_scan`** 直接服务补偿任务与到期清理的扫描条件，避免全表扫。

配套后台任务（P1）：到期清理（`deleted_at < now - trashRetainDays`）→ Purge；孤儿反向清理（`Lister` 列出对象但库里无 `storage_key` 匹配，且创建时间超 24h）→ 告警不自动删（宁可留垃圾，不可误删）。

---

## 13. 导入场景：Excel 校验失败的标识与处置

需求："Excel 上传后，如果校验不通过，还得有标识。" 这个需求要先**拆成两层**，因为它们的拦截时机、责任方、落库位置完全不同：

| 层 | 拦截时机 | 责任方 | 失败表现 |
|---|---|---|---|
| **格式校验** | 上传请求还没处理完 | FileManager（§8.1） | 40012，**文件根本进不了系统**，无任何记录 |
| **内容校验** | 下载解析之后 | 导入业务（`application/import`） | 批次 `status=failed` + 错误行明细，**文件保留** |

扩展名是 `.xlsx` 但内容是伪造的（MIME 嗅探不通过）→ 格式层就死了，这一层 v1.0 就有。而"第 5 行手机号格式不对、第 8 行部门不存在"这种，**只有把 Excel 读出来解析才知道**——它校验的是 Excel 里的业务数据，不是文件本身。这层是 v1.2 补齐的。

### 13.1 标识为什么不加进 `sys_file`

直觉做法是给 `sys_file` 加个 `status`（校验失败/已导入）。**明确拒绝**，两个理由：

1. **`sys_file` 的每一列都必须对全部 biz 成立**。"校验失败"对头像、合同、站点 logo 意味着什么？没有意义。一旦开始为某个 biz 加业务状态列，表就会慢慢变成所有业务的杂物抽屉——这正是 §2.1"库内存 key 不存 URL"要防的同一种腐化；
2. **校验结果是"导入批次"的事实，不是"文件"的事实**。同一个文件理论上可以被两个导入批次引用（上传一次、建两个批次），批次 A 校验失败、批次 B 通过——文件该标什么？没法标。

正确落点：**`sys_import_batch`，以 `file_id` 关联 `sys_file`**。文件列表页（按 `sys_file` 查）看到的是干净的原始文件；导入列表页（按批次查）才显示失败标识。想在文件列表也带失败角标，接口 join 批次表即可，不动表结构。

### 13.2 `sys_import_batch` 表设计（P1）

```sql
CREATE TABLE sys_import_batch (
  id           BIGINT      NOT NULL COMMENT '雪花ID',
  file_id      BIGINT      NOT NULL COMMENT '关联 sys_file.id（该文件 biz=import）',
  import_type  VARCHAR(32) NOT NULL COMMENT '导入目标业务域 employee/attendance/...（值域开放，随业务增长；区别于 sys_file.biz 的固定枚举，见 13.2.1）',
  status       VARCHAR(16) NOT NULL COMMENT 'parsed/validated/failed/imported',
  total_rows   INT         NOT NULL DEFAULT 0,
  ok_rows      INT         NOT NULL DEFAULT 0,
  error_rows   INT         NOT NULL DEFAULT 0,
  error_detail MEDIUMTEXT  NULL COMMENT '[{"row":5,"col":"mobile","code":30025},...]',
  created_by   BIGINT      NOT NULL DEFAULT 0,
  created_at   DATETIME(3) NOT NULL,
  finished_at  DATETIME(3) NULL,
  PRIMARY KEY (id),
  KEY idx_file (file_id),
  KEY idx_type_created (import_type, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='导入批次（校验结果与失败标识）';
```

#### 13.2.1 `import_type` ≠ `sys_file.biz`（容易看混，特意说明）

两个 `biz` 长得像但**粒度和值域都不同**，改名 `import_type` 就是为了断掉混淆：

| | `sys_file.biz` | `sys_import_batch.import_type` |
|---|---|---|
| 回答的问题 | 这个文件**是什么用途** | 这批数据**导入到哪个业务** |
| 值域 | **封闭枚举**（7 个，定义在 `domain/file.Biz`）：avatar / resume / contract / import / export / site / temp | **开放值域**，随业务增长：employee / attendance / salary / ... |
| 谁维护 | 代码（加值要改 domain 常量） | 业务模块（新增导入功能时自己定义） |
| 影响什么 | key 前缀（`import/2026/...`）、PublicURL 权限、删除策略 | 校验规则、Excel 模板、用哪个导入处理器 |

关系链：上传导入文件 → `sys_file.biz = 'import'`，key 前缀 `import/...`；建批次 → `sys_import_batch.import_type = 'employee'`。**一个文件（biz=import）对应一个批次（本期 1:1）**，批次通过 `file_id` 关联拿文件信息（origin_name、签名 URL）。

为什么不合并成细粒度枚举（把 `employee`/`attendance` 直接塞进 `sys_file.biz`）：`Biz` 枚举会随导入功能无限增长（每加一种导入就改 domain 常量），且 key 空间从统一的 `import/` 前缀裂成 `employee/`、`attendance/`...，"导入类文件"这个归类就没了——到期清理、格式校验、按用途统计都变得零散。**存储维度用粗枚举，业务维度用细值域，各归各的表。**

#### 13.2.2 字典化取舍（配套 `config/sql/sys_dict_init.sql`）

判断标准沿用菜单文档 §1.1："这份文案谁维护？"（字典模块本身已独立成篇：`docs/系统管理-字典(sys_dict)设计.md`，其 §0 收录同一套判据表）

| 字段 | 走哪里 | 理由 |
|---|---|---|
| `sys_import_batch.import_type` | **字典** `import_type`（已启用） | 开放值域，新导入功能插一条字典项即出现下拉，无需发版 |
| `sys_file.biz`（7 个） | 前端语言包（i18n key `file.biz.*`） | 封闭代码枚举，文案归开发随代码发版，三语随代码一次到位；不需要运营为每个枚举补译文 |
| `sys_file.driver`、文件状态（`deleted_at`/`purged_at` 派生）、`sys_import_batch.status` | 前端语言包 | 同上，封闭枚举；`file_status` 非库字段，是两列派生态 |

值域校验**永远以代码常量为准**，字典/语言包只管展示与下拉取值——运营在字典里加的项后端不会接受，安全兜底在 domain 层。`sys_dict_init.sql` 附有"统一字典化"可选段（默认注释），若决定所有下拉统一走字典，启用它并同步排期字典 i18n 子表。

状态机与文件生命周期的对应关系：

```
上传完成 ──→ parsed（行数已统计）
        ├─→ validated（全部通过）──→ imported
        └─→ failed（error_rows>0，error_detail 非空）★ 这就是"校验不通过的标识"
```

关键约定：

- **`error_detail` 里的失败原因存错误码**（如 `"code": 30025`），不存中文文案——前端拿到后走 i18n 渲染，与 §3.3"用户可见文案不散落在代码里"一致；`col` 存列名字段，行号 `row` 从 2 起（Excel 表头占第 1 行）；
- **失败批次不覆盖、不重试**：修正后重新上传 = 新文件 + 新批次。导入历史是审计事实，覆盖历史等于毁灭证据；
- `total/ok/error` 三行数字在解析时一次算完，**不做"边导边记"**——HR 导入是千行级，一次事务内全量校验再决定是否入库，比"导了 800 行发现第 801 行坏了"好得多（后者需要回滚或部分导入语义，复杂度爆炸）。**全过才导，一错全不导**，默认原子。

### 13.3 校验失败后，文件怎么办（接 §9 删除语义）

三种策略，默认 A：

| 策略 | 动作 | 适用 |
|---|---|---|
| **A. 保留 + 引用计数保护（默认）** | 建批次时 `IncRef`；批次状态落定（failed/validated/imported）且用户确认不再需要、或超过保留期（建议 7 天）时 `DecRef` → `ref_count=0` 后才允许 `SoftDelete` → 30 天后到期清理 | 所有场景。文件是"现场证据"：用户 disput"我传的不是这个文件"时，原始文件就是唯一物证 |
| **B. 立即软删** | 导入界面提供"删除重传"按钮 → `SoftDelete` | 明显传错文件的场景，用户主动触发 |
| **C. 校验失败即 Purge** | 不推荐 | — |

**校验期间 `IncRef` 是硬要求**：解析可能耗时数秒，期间若另一个管理员把文件删了，解析协程拿到的是半途消失的对象（local 是 404，OSS 是 NoSuchKey）。IncRef → ref_count>0 → §9.3 的删除保护自动生效，不需要 FileManager 侧新增任何"解析锁"机制。这正是 v1.1 引用计数设计的第一个真实收益。

**C 为什么不推荐**：purge 不可逆且无物证。错误明细只说"第 5 行手机号格式错"，但"第 5 行原来的内容是什么"只有原始文件能回答。宁可多留 7 天垃圾，不留一笔糊涂账。

### 13.4 接口与前端标识

```
GET /api/v1/admin/imports?importType=employee&status=failed&page=1&pageSize=20
  → {list: [{id, biz, status, totalRows, okRows, errorRows,
             fileName: "花名册-1105.xlsx",     ← sys_file.origin_name
             fileUrl: "签名URL",               ← 现签，可下载原始文件
             errorDetail: [{row, col, code}],  ← 首页错误可选内联，明细分页另拉
             createdAt}], total, page, pageSize}

GET /api/v1/admin/imports/:id/errors?page=1   ← 错误行明细分页（千行 Excel 错一半时不能一次吐完）
```

前端标识：导入记录列表 `status=failed` → 红色 Tag"校验失败 · 23 行错误"，点击展开错误明细（行号 + 列 + 本地化文案），并提供两个动作：下载原始文件（对账）、重新上传（新批次）。

### 13.5 验收补充（并入 §14 P1）

- 上传含错误行的 Excel → 批次 `status=failed`、`error_detail` 含行号与错误码 → 原始文件仍可签名下载；
- 批次未 `DecRef` 时对文件 `SoftDelete` → 40015；`DecRef` 后 → 成功进回收站；
- `error_detail` 里的文案在前端按语言包渲染，切语言后错误原因跟随变化；
- 全过才导：第 N 行校验失败时，前 N-1 行**不产生任何数据写入**。

---

## 14. 实施分期与验收

**P0（可用：上传 + 读取 + 软删）**
1. `domain/file`：`Store` / `Object` / `Biz` / `NewKey` / `ValidateKey` / `TrashKey` / 哨兵；
2. `infrastructure/storage/signer.go` + `local`：`Put`/`Get`/`Stat`/`Delete`/`Move`/`URL`/`PublicURL`；
3. `manager.go`：按 `storage.driver` 装配，未实现 driver 返回明确错误；
4. `sys_file` 表 + dao + `application/file.Service`：Upload / Get / URL / SoftDelete / Restore / BatchDelete；
5. 上传、签名下载、列表、删除、恢复五个 handler；错误码八条五处同步；配置校验；
6. 验收：
   - 上传 → 返回 `{id, path, url}` → 刷新页面 url 仍可开；
   - 把 url 的 `s=` 改一个字符 → 403；过期后 → 403；`../` 拼 key → 400；
   - 软删后 `Get` → 40010；`Restore` → 又能读；
   - `ref_count=1` 时软删 → 40015；归零后 → 成功；
   - 批量删 3 条（含 1 条被引用）→ 返回 `success:2, failed:[...]`，成功的确实没了；
   - 非超管调 purge → 40017；超管 purge → 对象消失、行留墓碑、操作日志 detail 是整行 JSON。

**P1（对齐：三 driver + 彻底删）**
7. `oss` + `minio` driver（新增两个 SDK 依赖，见下）；
8. 同一套验收脚本（含删除/恢复/purge）在 local/oss/minio 三份配置下各跑一遍——**这是"可切换"唯一的验收标准**；
9. Redis tombstone（local 吊销）+ `strictRevoke` 生效验证：软删后立刻访问原 url 必须 404（三个 driver 都要过）；
10. 补偿任务 + 到期清理任务；`sys_config` 的 `site_logo`/`site_bg` 改为存 key；
11. `sys_import_batch` 表 + 导入批次接口（§13.4：列表/错误明细分页），校验失败标识与 `IncRef` 保护落地；
12. 前端：文件列表 / 回收站 / 恢复 / 批量删除 / 二次确认弹窗（purge 必须输入"确认删除"或勾选不可逆提示）；导入记录列表含"校验失败"标识与错误明细展开。

**P2（增强）**
12. `readFallback` 只读回源；`cmd/filemigrate` 迁移工具；
13. 孤儿文件扫描与容量统计（`Lister`）。

**新依赖声明**（规范铁律 7）：

| 依赖 | 用途 | 为什么不用替代方案 |
|---|---|---|
| `github.com/aliyun/aliyun-oss-go-sdk/oss` v3 | OSS 驱动 | 自实现 V4 签名易错且难维护；OSS 的 S3 兼容层与原生行为有差异，不用 S3 SDK 硬套 |
| `github.com/minio/minio-go/v7` v7 | MinIO 驱动 | 官方 SDK，Presign/Range/Copy 都现成 |

仅被对应 driver 文件 import；未选中的 driver 不会初始化客户端、不会建立连接。实现时以 `go get` 实际解析版本为准，并在交付说明登记。

---

## 15. 明确不做

- 分片上传 / 断点续传（>100MB 场景出现时再谈，届时 local 与 OSS 的语义差异要重新对齐）；
- 图片压缩、缩略图、水印（应由上传侧或 CDN 图片处理承担）；
- 运行时热切换 driver（§2.3）；
- 跨 driver 双写（一致性成本远高于收益，迁移工具更诚实）；
- 公开读桶（HR 业务默认私有，`PublicURL` 仅限于 `BizSite`）；
- **对象版本控制 / 多版本恢复**（S3 的 Versioning 与 MinIO、本地三者行为差异太大，靠 `sys_file` 单版本 + 回收站已够用）；
- **按 key 批量删（不存在的 key 静默跳过）** —— 删除一律按 `sys_file.id`，key 是内部标识，不能当操作入口。

---

## 16. 变更记录

| 版本 | 日期 | 变更 |
|---|---|---|
| v1.0 | 2026-11-10 | 初稿：三 driver 端口定义、key 规范、签名 URL、安全清单、4001x 错误码、分期实施 |
| v1.1 | 2026-11-10 | 新增 §9 删除设计：三层语义（软删/恢复/彻底删）、逻辑 key 与物理 key 双层、引用计数、批量删除部分成功语义、签名撤销窗口与三档对策、失败补偿与到期清理、审计对齐 `sys_oper_log`；`sys_file` 表从 P1 提前为 P0 并补 `storage_key`/`purged_at`/`ref_count`/`purge_attempts`；`Store` 增加 `Move` 与可选 `Lister`；下载路由改为 `files/content/*key`（修复 gin 通配与参数路由冲突，白名单边界收窄）；错误码扩至 40017 |
| v1.2 | 2026-11-10 | 新增 §13 导入场景：内容校验失败的标识落 `sys_import_batch`（明确拒绝加进 `sys_file`，理由两条）、批次状态机、`error_detail` 存错误码走 i18n、"全过才导"原子导入、校验期 `IncRef` 保护（引用计数的首个真实收益）、三种失败处置策略（默认保留+延迟清理）；P1 增加 `sys_import_batch` 与导入接口；后续章节号顺延。批次业务域字段命名 `import_type`（开放值域），与 `sys_file.biz`（封闭枚举）区分，见 §13.2.1；字典化取舍见 §13.2.2（`import_type` 走字典，封闭枚举走语言包），初始化 SQL 落 `config/sql/sys_dict_init.sql` |
| v1.3 | 2026-11-10 | key 格式升级：`{snowflakeID}{ext}` → `{snowflakeID}-{safeName}{ext}`，清洗后的原文件名进 key 提升存储侧可读性（OSS 控制台/本地目录肉眼可辨）。清洗六规则（剥路径成分、删非法字符、空白折叠、空则省略、60 rune 截断、扩展名单列）；唯一性仍由雪花 ID 保证，safeName 重复/缺失无害；`ValidateKey` 正则放宽为后缀可选（兼容无后缀 key），`NewKey(biz, originName)` 签名变更；`sys_file.origin_name` 仍存未清洗原始名，`Content-Disposition` 行为不变 |
