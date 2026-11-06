// 编译期内嵌 DDL（config/sql/*.sql）的迁移执行。
// 业务参考：docs/SaaS管理后台-管理账号(sys_user)设计.md §11 P0“两张表 + 迁移”。
package dao

import (
	"context"
	"fmt"
	"time"

	conf "dugu-hr-saas-admin/config"
)

// migrate 幂等执行迁移（CREATE TABLE IF NOT EXISTS）。
// 迁移与 seed 都必须经会话克隆执行——根连接 _db 的 Statement 是共享可变状态，
// 在其上直接调用 finisher（Create/First/...）会把 schema 污染带给后续所有克隆。
func migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, f := range conf.SQLFiles {
		sql, err := conf.ReadSQL(f)
		if err != nil {
			return fmt.Errorf("读内嵌迁移 %s: %w", f, err)
		}
		if err := NewDBClient(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("执行迁移 %s: %w", f, err)
		}
	}
	return nil
}
