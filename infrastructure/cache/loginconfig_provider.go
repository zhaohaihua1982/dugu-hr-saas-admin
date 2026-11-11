// loginconfig.Provider 实现（docs 登录配置设计 §4 三级缓存 + AGENTS §3.5 PolicyProvider）：
//
//	进程内存（30s TTL） → Redis login:config:{type}（5min TTL） → MySQL sys_login_config
//	全 miss → 代码兜底默认值（§3.3 最严默认）+ WARN 告警。
//
// 失效：管理端 Save 成功 → Invalidate 删 Redis 键；进程内存 30s TTL 是唯一容忍窗口。
package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"dugu-hr-saas-admin/domain/loginconfig"
)

// 配置缓存键与 TTL（§4）。
const (
	keyLoginConfig    = "login:config:" // login:config:{type} → 快照 JSON
	loginCfgRedisTTL  = 5 * time.Minute
	loginCfgMemoryTTL = 30 * time.Second
)

// cachedRow Redis 快照 JSON 形状（Config 平铺 + 版本/时间）。
type cachedRow struct {
	*loginconfig.Config
	SavedAt time.Time `json:"saved_at"`
}

// memEntry 进程内快照。
type memEntry struct {
	snap     loginconfig.Snapshot
	expireAt time.Time
}

// LoginConfigProvider 三级缓存读取器。
type LoginConfigProvider struct {
	repo loginconfig.Repository
	rdb  *Client
	mu   sync.Mutex
	mem  map[loginconfig.ConfigType]memEntry
}

// NewLoginConfigProvider 构造。
func NewLoginConfigProvider(repo loginconfig.Repository, rdb *Client) *LoginConfigProvider {
	return &LoginConfigProvider{repo: repo, rdb: rdb, mem: make(map[loginconfig.ConfigType]memEntry)}
}

var _ loginconfig.Provider = (*LoginConfigProvider)(nil)

// Snapshot 三级读取（§4）。DB 读失败不阻断登录——回落兜底默认值并告警。
func (p *LoginConfigProvider) Snapshot(ctx context.Context, t loginconfig.ConfigType) (loginconfig.Snapshot, error) {
	// 1. 进程内存
	p.mu.Lock()
	if e, ok := p.mem[t]; ok && time.Now().Before(e.expireAt) {
		p.mu.Unlock()
		return e.snap, nil
	}
	p.mu.Unlock()

	snap := p.load(ctx, t)

	// 回填进程内存（30s）
	p.mu.Lock()
	p.mem[t] = memEntry{snap: snap, expireAt: time.Now().Add(loginCfgMemoryTTL)}
	p.mu.Unlock()
	return snap, nil
}

// load 二/三级读取 + 兜底。
func (p *LoginConfigProvider) load(ctx context.Context, t loginconfig.ConfigType) loginconfig.Snapshot {
	// 2. Redis（故障静默降级到 MySQL）
	if p.rdb != nil {
		raw, rErr := p.rdb.raw().Get(ctx, keyLoginConfig+itoa(int64(t))).Result()
		if rErr == nil && raw != "" {
			var row cachedRow
			if jErr := json.Unmarshal([]byte(raw), &row); jErr == nil && row.Config != nil {
				return loginconfig.Snapshot{Config: row.Config, Version: row.Version, ReadAt: time.Now()}
			}
			slog.Warn("登录配置 Redis 快照反序列化失败（降级 DB）", "type", t, "err", rErr)
		}
	}

	// 3. MySQL
	cfg, err := p.repo.ByType(ctx, t)
	if err == nil && cfg != nil {
		p.storeRedis(ctx, t, cachedRow{Config: cfg, SavedAt: time.Now()})
		return loginconfig.Snapshot{Config: cfg, Version: cfg.Version, ReadAt: time.Now()}
	}
	if err != nil { // DB 整体不可用：跑在兜底默认值上，绝不拒登（§4 失效规则 2/§8）
		slog.Warn("登录配置 DB 读取失败，回落代码兜底默认值（需告警）", "type", t, "err", err)
		return fallback(t)
	}

	// 行不存在（seed 漏跑/表损坏）：兜底 + WARN（§8 边界情况 1）
	slog.Warn("登录配置行不存在（seed 漏跑或表损坏），使用代码兜底默认值（需告警）", "type", t)
	return fallback(t)
}

// fallback 代码兜底默认值（§3.3：语义是"最严默认"，不是"无配置"）。
func fallback(t loginconfig.ConfigType) loginconfig.Snapshot {
	return loginconfig.Snapshot{Config: loginconfig.Defaults(t), Fallback: true, ReadAt: time.Now()}
}

// storeRedis 回填 Redis（失败只 WARN：TTL 5min + 进程内存兜底，§4）。
func (p *LoginConfigProvider) storeRedis(ctx context.Context, t loginconfig.ConfigType, row cachedRow) {
	if p.rdb == nil {
		return
	}
	b, err := json.Marshal(row)
	if err != nil {
		slog.Warn("登录配置 Redis 序列化失败", "type", t, "err", err)
		return
	}
	if err := p.rdb.raw().Set(ctx, keyLoginConfig+itoa(int64(t)), b, loginCfgRedisTTL).Err(); err != nil {
		slog.Warn("登录配置 Redis 回填失败（DB 已命中，不影响生效）", "type", t, "err", err)
	}
}

// Invalidate 保存成功后失效：删 Redis 键 + 清进程内存（§4 失效规则 1）。
// 删缓存失败 → 5min TTL 兜底过期（WARN，§8）。
func (p *LoginConfigProvider) Invalidate(ctx context.Context, t loginconfig.ConfigType) error {
	p.mu.Lock()
	delete(p.mem, t)
	p.mu.Unlock()
	if p.rdb == nil {
		return nil
	}
	if err := p.rdb.raw().Del(ctx, keyLoginConfig+itoa(int64(t))).Err(); err != nil {
		slog.Warn("删除登录配置缓存失败（TTL 兜底过期）", "type", t, "err", err)
		return err
	}
	return nil
}
