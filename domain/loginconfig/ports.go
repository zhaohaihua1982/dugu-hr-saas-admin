// 登录配置端口：仓储（管理端读写）与 Provider（管线快照，§4 三级缓存）。
package loginconfig

import (
	"context"
	"time"
)

// Repository 配置仓储端口（infrastructure/db/dao 实现）。
// 配置行 ≤ 2 行、不可删、无软删——端口只有读/存两件事。
type Repository interface {
	// ByType 按 (config_type, scope_id=0) 取唯一行；未命中返回 (nil, nil)。
	ByType(ctx context.Context, t ConfigType) (*Config, error)
	// Save 全量保存（乐观锁：WHERE id=? AND version=?）。
	// 影响行数=0 时实现内复查该行：行已消失 → ErrNotFound，版本不符 → ErrVersionConflict。
	// 成功返回新版本号（旧 version+1）。
	Save(ctx context.Context, c *Config) (int, error)
	// Create 建行（reset 时行缺失的兜底路径；ID 取类型固定 seed ID）。
	Create(ctx context.Context, c *Config) error
}

// Snapshot 管线消费的策略快照（§4：读不到任何一层 → 代码兜底默认值 + 告警）。
type Snapshot struct {
	Config   *Config
	Fallback bool      // true=DB/缓存全 miss，跑在 §3.2 代码兜底默认值上（管理端展示横幅）
	Version  int       // 0=兜底
	ReadAt   time.Time // 快照装配时间
}

// Provider 登录策略快照端口（AGENTS §3.5 PolicyProvider 惯例：内存快照 + TTL，
// Save 后失效；业务管线只经它读配置，禁止直查 sys_login_config）。
type Provider interface {
	// Snapshot 返回当前生效策略（三级：进程内存 30s → Redis 5min → MySQL；
	// 全 miss → 兜底默认值 Fallback=true，实现内告警）。
	Snapshot(ctx context.Context, t ConfigType) (Snapshot, error)
	// Invalidate 管理端保存成功后失效缓存（进程内存窗口 ≤30s 是唯一容忍，§4）。
	Invalidate(ctx context.Context, t ConfigType) error
}
