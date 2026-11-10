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

// columnSpecs 新列增量（缺失 → ADD COLUMN；幂等）。
var columnSpecs = []struct {
	table  string
	column string
	def    string
}{
	// v0.5：id_path——父链 ID 串（根=自身；子="父id/.../自身"），供前端展开折叠/子树过滤。
	// 深度≤20 等深树撑爆风险：VARCHAR(2048) 余量；由应用层在 Create/Move 维护，迁移只补列。
	{table: "sys_menu", column: "id_path", def: "VARCHAR(2048) NOT NULL DEFAULT ''"},
}

// ensureColumns 幂等补列。
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
		if cnt > 0 {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN `%s` %s", spec.table, spec.column, spec.def)
		if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("新增列 %s.%s: %w", spec.table, spec.column, err)
		}
		slog.Info("迁移增量列已添加", "table", spec.table, "column", spec.column)
	}
	return nil
}

// renameSpecs 列改名（保数据；改名后旧名即不存在，天然幂等）。
var renameSpecs = []struct {
	table string
	from  string
	to    string
}{
	{"sys_menu", "action_code", "code"}, // v0.4：按运营侧命名习惯存 code
}

// Migrate 幂等执行 config/sql 中的 DDL（docs §11 P0“表+迁移”）。
// 增量三段式（均已幂等）：
//  1. dropColumns：退役列清理（v0.4 撤销生成列后，唯一键直接建在 (parent_id,<col>,deleted_at)）；
//  2. dropIndexSpecs：退役索引清理（如 v0.3 前的旧 uk_action 全局唯一）；
//  3. ensureIndexes：唯一/普通索引增量（information_schema 检查后 CREATE/
//     CREATE UNIQUE INDEX）。
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
	if err := d.dropColumns(ctx); err != nil { // 先退生成列：解除“列被生成列引用无法 RENAME”（MySQL 3108）
		return err
	}
	if err := d.renameColumns(ctx); err != nil {
		return err
	}
	if err := d.ensureColumns(ctx); err != nil { // 新列增量（如 v0.5 id_path）
		return err
	}
	if err := d.dropIndexes(ctx); err != nil {
		return err
	}
	return d.ensureIndexes(ctx)
}

// columnDropSpecs 退役列清理（结构演进淘汰；存在即删，幂等）。
var columnDropSpecs = []struct {
	table  string
	column string
}{
	// v0.4：撤销生成列方案——唯一索引直接建在 (parent_id,<col>,deleted_at)，
	// docs 仓库惯例“软删参与唯一键”（sys_user/sys_dict 同款），不留多余字段。
	{table: "sys_menu", column: "action_uniq"},
	{table: "sys_menu", column: "menu_name_uniq"},
	{table: "sys_menu", column: "menu_path_uniq"},
}

// dropColumns 幂等清理退役列（索引随列自动消失）。
func (d *DB) dropColumns(ctx context.Context) error {
	for _, spec := range columnDropSpecs {
		var cnt int64
		check := d.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM information_schema.columns
			  WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			spec.table, spec.column).Scan(&cnt)
		if check.Error != nil {
			return fmt.Errorf("检查列 %s.%s: %w", spec.table, spec.column, check.Error)
		}
		if cnt == 0 {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", spec.table, spec.column)
		if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("删除列 %s.%s: %w", spec.table, spec.column, err)
		}
		slog.Info("迁移已删除退役列", "table", spec.table, "column", spec.column)
	}
	return nil
}

// renameColumns 幂等列改名（RENAME COLUMN 保数据；改名即幂等）。
func (d *DB) renameColumns(ctx context.Context) error {
	for _, spec := range renameSpecs {
		var cnt int64
		check := d.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM information_schema.columns
			  WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			spec.table, spec.from).Scan(&cnt)
		if check.Error != nil {
			return fmt.Errorf("检查列 %s.%s: %w", spec.table, spec.from, check.Error)
		}
		if cnt == 0 {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE `%s` RENAME COLUMN `%s` TO `%s`", spec.table, spec.from, spec.to)
		if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("改名列 %s.%s→%s: %w", spec.table, spec.from, spec.to, err)
		}
		slog.Info("迁移已改名列", "table", spec.table, "from", spec.from, "to", spec.to)
	}
	return nil
}

// dropIndexSpecs 退役索引（结构演进淘汰；存在即删，幂等）。
var dropIndexSpecs = []struct {
	table string
	name  string
}{
	{"sys_menu", "uk_action"},      // v0.3：全局唯一 → 同级唯一（生成列时代的旧键）
	{"sys_menu", "uk_menu_action"}, // v0.4：uk_menu_action → uk_menu_code(type,code)
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
	// v0.4 同级唯一三键：直接列 + deleted_at（软删参与唯一键，docs 仓库惯例）
	{table: "sys_menu", name: "uk_menu_code", column: "type, code, deleted_at", unique: true},
	{table: "sys_menu", name: "uk_menu_name", column: "parent_id, name, deleted_at", unique: true},
	{table: "sys_menu", name: "uk_menu_path", column: "parent_id, path, deleted_at", unique: true},
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
