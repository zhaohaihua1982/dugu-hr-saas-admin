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

// Migrate 幂等执行 config/sql 中的 DDL（docs §11 P0“表+迁移”）。
// 增量三段式（均已幂等）：
//  1. ensureColumns：生成列/普通列增量（结构变化如“同级唯一 v0.3”；
//     modify 语义的 spec 每次启动重放 ALTER——菜单量级是百行，重建成本低）；
//  2. dropIndexSpecs：退役索引清理（如 v0.3 前的旧 uk_action 全局唯一）；
//  3. ensureIndexes：唯一/普通索引增量（information_schema 检查后 CREATE）。
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
	if err := d.ensureColumns(ctx); err != nil {
		return err
	}
	if err := d.dropIndexes(ctx); err != nil {
		return err
	}
	return d.ensureIndexes(ctx)
}

// columnSpecs 生成列/普通列增量。modify=true 的 spec 启动时重放 ALTER（幂等）。
var columnSpecs = []struct {
	table  string
	column string
	def    string
	modify bool
}{
	// v0.3 同级唯一三生成列：0x1F 拼 parent_id；空值/软删行 → NULL 不参与。
	{table: "sys_menu", column: "action_uniq",
		def: "VARCHAR(100) GENERATED ALWAYS AS (CASE WHEN action_code <> '' AND deleted_at = 0 THEN CONCAT(parent_id, 0x1F, action_code) ELSE NULL END) STORED", modify: true},
	{table: "sys_menu", column: "menu_name_uniq",
		def: "VARCHAR(100) GENERATED ALWAYS AS (CASE WHEN deleted_at = 0 THEN CONCAT(parent_id, 0x1F, name) ELSE NULL END) STORED", modify: false},
	{table: "sys_menu", column: "menu_path_uniq",
		def: "VARCHAR(176) GENERATED ALWAYS AS (CASE WHEN deleted_at = 0 AND path <> '' THEN CONCAT(parent_id, 0x1F, path) ELSE NULL END) STORED", modify: false},
}

// ensureColumns 幂等补列：缺失 → ADD COLUMN；modify=true 且已存在 → MODIFY 重放定义。
func (d *DB) ensureColumns(ctx context.Context) error {
	for _, spec := range columnSpecs {
		var cnt int64
		check := d.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM information_schema.columns
			  WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			spec.table, spec.column).Scan(&cnt)
		if check.Error != nil {
			return fmt.Errorf("检查列 %s.%s: %w", spec.table, spec.column, check.Error)
		}
		switch {
		case cnt > 0 && !spec.modify:
			continue
		case cnt > 0:
			stmt := fmt.Sprintf("ALTER TABLE `%s` MODIFY COLUMN `%s` %s", spec.table, spec.column, spec.def)
			if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
				return fmt.Errorf("重放列 %s.%s: %w", spec.table, spec.column, err)
			}
			slog.Info("迁移列定义已重放", "table", spec.table, "column", spec.column)
		default:
			stmt := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", spec.table, spec.column, spec.def)
			if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
				return fmt.Errorf("新增列 %s.%s: %w", spec.table, spec.column, err)
			}
			slog.Info("迁移增量列已添加", "table", spec.table, "column", spec.column)
		}
	}
	return nil
}

// dropIndexSpecs 退役索引（结构演进淘汰；存在即删，幂等）。
var dropIndexSpecs = []struct {
	table string
	name  string
}{
	{"sys_menu", "uk_action"}, // v0.3：全局唯一 → 同级唯一 uk_menu_action
}

// dropIndexes 幂等清理退役索引。
func (d *DB) dropIndexes(ctx context.Context) error {
	for _, spec := range dropIndexSpecs {
		var cnt int64
		check := d.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM information_schema.statistics
			  WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
			spec.table, spec.name).Scan(&cnt)
		if check.Error != nil {
			return fmt.Errorf("检查索引 %s.%s: %w", spec.table, spec.name, check.Error)
		}
		if cnt == 0 {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE `%s` DROP INDEX `%s`", spec.table, spec.name)
		if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("删除索引 %s.%s: %w", spec.table, spec.name, err)
		}
		slog.Info("迁移已删除退役索引", "table", spec.table, "index", spec.name)
	}
	return nil
}

// indexSpecs 待保证的增量索引。新增索引在此登记（表/索引名/列清单/是否唯一）。
var indexSpecs = []struct {
	table  string
	name   string
	column string
	unique bool
}{
	{table: "sys_login_log", name: "idx_log_identifier", column: "identifier_mask, created_at"},
	// v0.3 同级唯一三键（依赖 ensureColumns 先行创建生成列）
	{table: "sys_menu", name: "uk_menu_action", column: "action_uniq", unique: true},
	{table: "sys_menu", name: "uk_menu_name", column: "menu_name_uniq", unique: true},
	{table: "sys_menu", name: "uk_menu_path", column: "menu_path_uniq", unique: true},
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
		createVerb := "CREATE INDEX"
		if spec.unique {
			createVerb = "CREATE UNIQUE INDEX"
		}
		stmt := fmt.Sprintf("%s `%s` ON `%s` (%s)", createVerb, spec.name, spec.table, spec.column)
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
