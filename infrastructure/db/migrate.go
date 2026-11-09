// 迁移与超管 seed：编译期内嵌 DDL，启动幂等执行。
// 两处都**必须**经 WithContext 克隆执行（见 db.WithContext 的使用纪律）。
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/sysuser"
)

// Migrate 幂等执行 config/sql 中的 DDL（docs §11 P0“两张表+迁移”）。
// v1.2 起包含**对已建表的幂等索引增量**：CREATE TABLE IF NOT EXISTS 不会给
// 已存在表补索引，因此在 DDL 执行后对 indexSpecs 逐个先查 information_schema
// 再 CREATE INDEX（MySQL 不支持 CREATE INDEX IF NOT EXISTS 语法）。
func (d *DB) Migrate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, f := range conf.SQLFiles {
		sql, err := conf.ReadSQL(f)
		if err != nil {
			return fmt.Errorf("读内嵌迁移 %s: %w", f, err)
		}
		if err := d.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("执行迁移 %s: %w", f, err)
		}
	}
	return d.ensureIndexes(ctx)
}

// indexSpecs 待保证的增量索引。新增索引在此登记（表/索引名/列清单）。
var indexSpecs = []struct {
	table  string
	name   string
	column string
}{
	{table: "sys_login_log", name: "idx_log_identifier", column: "identifier_mask, created_at"},
}

// ensureIndexes 幂等保证增量索引存在。
func (d *DB) ensureIndexes(ctx context.Context) error {
	for _, spec := range indexSpecs {
		var cnt int64
		check := d.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM information_schema.statistics
			  WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
			spec.table, spec.name).Scan(&cnt)
		if check.Error != nil {
			return fmt.Errorf("检查索引 %s.%s: %w", spec.table, spec.name, check.Error)
		}
		if cnt > 0 {
			continue
		}
		stmt := fmt.Sprintf("CREATE INDEX `%s` ON `%s` (%s)", spec.name, spec.table, spec.column)
		if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("创建索引 %s.%s: %w", spec.table, spec.name, err)
		}
		slog.Info("迁移增量索引已创建", "table", spec.table, "index", spec.name)
	}
	return nil
}

// SeedSuperAdmin 初始超管 seed（docs §8.3、§11 P0 验收标准；仅空库时创建）。
// 严格DDD：经仓储端口写入，绝不绕过领域层直连 SQL 拼实体。
func (d *DB) SeedSuperAdmin(ctx context.Context, users sysuser.Repository, b conf.BootstrapAdmin, hasher sysuser.PasswordHasher, crypto sysuser.MobileCrypto, ids authn.IDGenerator) error {
	existing, err := users.List(ctx, sysuser.ListQuery{Page: 1, PageSize: 1})
	if err != nil {
		return fmt.Errorf("seed 检查 sys_user: %w", err)
	}
	if existing.Total > 0 {
		return nil
	}

	mobile, err := sysuser.NormalizeMobile(b.Mobile)
	if err != nil {
		return fmt.Errorf("bootstrapAdmin.mobile 不合法: %w", err)
	}
	if err := sysuser.ValidatePassword(b.Password); err != nil {
		return errors.New("bootstrapAdmin.password 不满足密码策略（≥10 位且含两类字符）")
	}
	c, err := hasher.Hash(b.Password)
	if err != nil {
		return err
	}
	now := time.Now()
	c.PwdUpdatedAt = &now
	m, err := sysuser.BindMobile(mobile, crypto)
	if err != nil {
		return err
	}
	u := &sysuser.User{
		ID:         ids.New(),
		Username:   sysuser.SuperAdminUsername,
		Name:       b.Name,
		Email:      b.Email,
		Status:     sysuser.StatusActive,
		Credential: c,
		Mobile:     m,
	}
	if err := users.Create(ctx, u); err != nil {
		return fmt.Errorf("创建初始超管: %w", err)
	}
	slog.Warn("[bootstrap] 已创建初始超管账号 username=admin（密码见 bootstrapAdmin/SAAS_BOOTSTRAP_ADMIN_PASSWORD）手机=" + u.Mobile.Mask + "，请登录后尽快修改密码")
	return nil
}
