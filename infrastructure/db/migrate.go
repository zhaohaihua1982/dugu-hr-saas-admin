// 迁移与超管 seed：编译期内嵌 DDL，启动幂等执行。
// 两处都**必须**经 WithContext 克隆执行（见 db.WithContext 的使用纪律）。
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/domain/authn"
	di18n "dugu-hr-saas-admin/domain/i18n"
	"dugu-hr-saas-admin/domain/sysuser"
	"dugu-hr-saas-admin/infrastructure/db/po"
	pi18n "dugu-hr-saas-admin/pkg/i18n"
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
	// 登录配置 P1.5：首登强制改密标记列（docs 登录配置设计 §2.4；存量账号缺省 0=不强制）。
	{table: "sys_user", column: "pwd_force_change", def: "TINYINT(1) NOT NULL DEFAULT 0"},
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
//
// 收尾：backfillAndDropMenuI18n——v0.6 把旧译文表数据并入统一翻译表后 DROP。
func (d *DB) Migrate(ctx context.Context, ids authn.IDGenerator) error {
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
	if err := d.ensureIndexes(ctx); err != nil {
		return err
	}
	if err := d.backfillAndDropMenuI18n(ctx, ids); err != nil { // v0.6：旧译文表数据并入统一翻译表后退役（幂等）
		return err
	}
	if err := d.mergeI18nTextRows(ctx, ids); err != nil { // v0.7：统一表旧粒度（一语言一行）合并为 JSON 粒度（幂等）
		return err
	}
	return d.seedDict(ctx) // 字典 seed（INSERT IGNORE 幂等；结构迁移全部完成后执行）
}

// ---------- v0.6 译文存量迁移（docs 基础设施-数据文案多语言(i18n)设计.md §9） ----------

// legacyI18nRow v0.6 回填目标行的旧粒度形状（lang/content 列——v1.5 起 PO 已无这两列）。
// 回填写入的旧粒度行由随后执行的 mergeI18nTextRows（v0.7）在同一次启动内合并成新粒度。
type legacyI18nRow struct {
	ID        int64     `gorm:"primaryKey;column:id"`
	ResType   string    `gorm:"column:res_type"`
	ResID     int64     `gorm:"column:res_id"`
	Field     string    `gorm:"column:field"`
	Lang      string    `gorm:"column:lang"`
	Content   string    `gorm:"column:content"`
	DeletedAt int64     `gorm:"column:deleted_at"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 表名。
func (legacyI18nRow) TableName() string { return "sys_i18n_text" }

// backfillAndDropMenuI18n —— sys_menu_i18n → sys_i18n_text（res_type='menu'、field='name'）。
// 幂等三步，全库一次性执行：
//  1. 旧表不存在（全新库）→ 直接返回，什么都不做；
//  2. 有旧表 → 逐行回填（INSERT，已存在 (res_id,lang,deleted_at) 的行跳过；
//     deleted_at 原样透传——软删行的历史语义一并保留；雪花 ID 应用侧生成，不用 seed 9e15 段）；
//  3. 行数核对一致 → DROP 旧表；不一致 fail-fast（旧表保留，下次启动自然重试）。
//
// 前置条件：SQLFiles 已先行建好统一表（sys_i18n_text.sql 在 Migrate 首段执行）。
func (d *DB) backfillAndDropMenuI18n(ctx context.Context, ids authn.IDGenerator) error {
	var tblCnt int64
	if err := d.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM information_schema.tables
		  WHERE table_schema = DATABASE() AND table_name = 'sys_menu_i18n'`).Scan(&tblCnt).Error; err != nil {
		return fmt.Errorf("检查 sys_menu_i18n 存在性: %w", err)
	}
	if tblCnt == 0 {
		return nil // 全新库或已迁移完（旧表已退役）
	}

	type legacyRow struct {
		MenuID    int64  `gorm:"column:menu_id"`
		Lang      string `gorm:"column:lang"`
		Name      string `gorm:"column:name"`
		DeletedAt int64  `gorm:"column:deleted_at"`
	}
	var rows []legacyRow
	if err := d.WithContext(ctx).
		Table("sys_menu_i18n").
		Select("menu_id, lang, name, deleted_at").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("读旧译文表: %w", err)
	}

	type key struct {
		resID int64
		lang  string
		del   int64
	}
	var existing []struct {
		ResID     int64  `gorm:"column:res_id"`
		Lang      string `gorm:"column:lang"`
		DeletedAt int64  `gorm:"column:deleted_at"`
	}
	if err := d.WithContext(ctx).Table("sys_i18n_text").
		Select("res_id, lang, deleted_at").
		Where("res_type = ? AND field = ?", "menu", "name").
		Find(&existing).Error; err != nil {
		return fmt.Errorf("读统一译文表存量: %w", err)
	}
	seen := make(map[key]bool, len(existing))
	for _, e := range existing {
		seen[key{e.ResID, e.Lang, e.DeletedAt}] = true
	}

	var migrated int
	for _, r := range rows {
		if seen[key{r.MenuID, r.Lang, r.DeletedAt}] {
			continue
		}
		row := legacyI18nRow{
			ID: ids.New(), ResType: "menu", ResID: r.MenuID,
			Field: "name", Lang: r.Lang, Content: r.Name, DeletedAt: r.DeletedAt,
		}
		if err := d.WithContext(ctx).Create(&row).Error; err != nil {
			return fmt.Errorf("回填译文 menu/%d/%s: %w", r.MenuID, r.Lang, err)
		}
		migrated++
	}

	var oldCnt, newCnt int64
	if err := d.WithContext(ctx).
		Table("sys_menu_i18n").Count(&oldCnt).Error; err != nil {
		return fmt.Errorf("统计旧译文表: %w", err)
	}
	if err := d.WithContext(ctx).Table("sys_i18n_text").
		Where("res_type = ? AND field = ?", "menu", "name").Count(&newCnt).Error; err != nil {
		return fmt.Errorf("统计统一译文表: %w", err)
	}
	if oldCnt != newCnt {
		return fmt.Errorf("菜单译文迁移行数不一致: 旧表=%d 统一表=%d（旧表保留未 DROP，下次启动自动重试）", oldCnt, newCnt)
	}
	if err := d.WithContext(ctx).Exec("DROP TABLE IF EXISTS sys_menu_i18n").Error; err != nil {
		return fmt.Errorf("退役旧译文表 sys_menu_i18n: %w", err)
	}
	slog.Info("菜单译文已迁移至统一翻译表 sys_i18n_text，旧表 sys_menu_i18n 已退役",
		"migrated", migrated, "total", newCnt)
	return nil
}

// ---------- v0.7 统一译文表粒度合并（docs 基础设施-数据文案多语言(i18n)设计.md §9.4，v1.5） ----------

// mergeI18nTextRows —— 旧粒度（一语言一行，v1.4 及之前）→ 合并粒度（一行 = 资源×字段，
// 全部语言进 texts JSON + lang_mask/complete）。
// 幂等开关：lang 列存在性——合并收尾即删列，再次启动直接跳过；全新库（DDL 已是新粒度）同样跳过。
// 步骤：
//  1. 无 lang 列 → no-op；
//  2. 补 texts/lang_mask/complete 三列（texts 迁移窗口暂允许 NULL，收尾全部有值）；
//  3. 读 lang 非空的活行，按 (res_type,res_id,field) 分组：默认语言键丢弃（§7.4 两份真相，
//     主表列承接）、无位映射语言丢弃（计数入日志，正常为 0）；
//  4. 事务内逐组：DELETE 该组全部行（含历史墓碑）→ 有残留译文则 INSERT 合并行
//     （created/updated 取组内最早/最晚，保留历史时间感）；
//  5. 清扫 texts IS NULL 残留行（软删墓碑、默认语言孤立组——旧粒度墓碑不迁移，
//     审计以 sys_oper_log 为准）；
//  6. 删旧索引 uk_i18n/idx_i18n_batch 与 lang/content 列，按新形状重建索引。
func (d *DB) mergeI18nTextRows(ctx context.Context, ids authn.IDGenerator) error {
	const table = "sys_i18n_text"
	if ok, err := d.hasColumn(ctx, table, "lang"); err != nil {
		return err
	} else if !ok {
		return nil // 全新库或已合并
	}
	for _, col := range []struct{ name, def string }{ // 2. 补列（幂等）
		{"texts", "`texts` JSON NULL"},
		{"lang_mask", "`lang_mask` BIGINT NOT NULL DEFAULT 0"},
		{"complete", "`complete` TINYINT NOT NULL DEFAULT 0"},
	} {
		if err := d.addColumnIfMissing(ctx, table, col.name, col.def); err != nil {
			return err
		}
	}

	// 3. 读活行并分组
	type legacyRow struct {
		ResType   string    `gorm:"column:res_type"`
		ResID     int64     `gorm:"column:res_id"`
		Field     string    `gorm:"column:field"`
		Lang      string    `gorm:"column:lang"`
		Content   string    `gorm:"column:content"`
		CreatedAt time.Time `gorm:"column:created_at"`
		UpdatedAt time.Time `gorm:"column:updated_at"`
	}
	var rows []legacyRow
	if err := d.WithContext(ctx).Table(table).
		Where("deleted_at = 0 AND lang IS NOT NULL AND lang <> ''").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("读统一表旧粒度行: %w", err)
	}
	type groupKey struct {
		resType string
		resID   int64
		field   string
	}
	type group struct {
		texts   map[string]string
		created time.Time
		updated time.Time
	}
	groups := map[groupKey]*group{}
	skipDefault, skipNoBit := 0, 0
	for _, r := range rows {
		l := di18n.Lang(r.Lang)
		if pi18n.Lang(l) == pi18n.Default() { // 默认语言键丢弃：主表列是唯一真相（§7.4）
			skipDefault++
			continue
		}
		if _, ok := di18n.LangBit(l); !ok { // 无位映射语言：lang_mask 无法表达，丢弃并计数
			skipNoBit++
			continue
		}
		k := groupKey{r.ResType, r.ResID, r.Field}
		g := groups[k]
		if g == nil {
			g = &group{texts: map[string]string{}, created: r.CreatedAt, updated: r.UpdatedAt}
			groups[k] = g
		}
		g.texts[r.Lang] = r.Content
		if r.CreatedAt.Before(g.created) {
			g.created = r.CreatedAt
		}
		if r.UpdatedAt.After(g.updated) {
			g.updated = r.UpdatedAt
		}
	}

	// 4. 事务逐组：删旧行 → 有译文则插合并行（删除与插入必须原子：
	// 中途崩溃重跑时组内 lang 行已删会导致译文丢失，靠事务保证重跑安全）。
	// 旧粒度兼容列（lang NOT NULL 无默认 / content）仍存在时，INSERT 必须显式带值
	// ——否则 Error 1364（迁移窗口内由 mergeI18nLegacyColumns 检测，列删除后自动走纯净分支）。
	var mergedRows int
	hasLang, err := d.hasColumn(ctx, table, "lang")
	if err != nil {
		return err
	}
	hasContent, err := d.hasColumn(ctx, table, "content")
	if err != nil {
		return err
	}
	for k, g := range groups {
		if err := d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(
				"DELETE FROM sys_i18n_text WHERE res_type = ? AND res_id = ? AND field = ?",
				k.resType, k.resID, k.field).Error; err != nil {
				return err
			}
			if len(g.texts) == 0 {
				return nil
			}
			mask, complete := mergeMaskOf(g.texts)
			row := po.SysI18nText{
				ID: ids.New(), ResType: k.resType, ResID: k.resID, Field: k.field,
				Texts: mergeTextsJSON(g.texts), LangMask: mask, Complete: complete,
				CreatedAt: g.created, UpdatedAt: g.updated,
			}
			if !hasLang && !hasContent {
				return tx.Create(&row).Error
			}
			// 迁移窗口分支：旧粒度列还在——lang 给空串（合并行不再按语言行参与旧 uk 语义）、content 给 NULL
			cols := []string{"id", "res_type", "res_id", "field", "texts", "lang_mask", "complete", "deleted_at"}
			args := []interface{}{row.ID, row.ResType, row.ResID, row.Field, row.Texts, row.LangMask, row.Complete, 0}
			if hasLang {
				cols = append(cols, "lang")
				args = append(args, "")
			}
			if hasContent {
				cols = append(cols, "content")
				args = append(args, "") // content NOT NULL：迁移窗口兼容列给空串
			}
			cols = append(cols, "created_at", "updated_at")
			stmt := "INSERT INTO sys_i18n_text (" + strings.Join(cols, ",") + ") VALUES ("
			for range args {
				stmt += "?,"
			}
			stmt += "NOW(3),NOW(3))"
			return tx.Exec(stmt, args...).Error
		}); err != nil {
			return fmt.Errorf("合并译文组 %s/%d/%s: %w", k.resType, k.resID, k.field, err)
		}
		mergedRows++
	}

	// 5. 清扫未合并残留（texts 仍为 NULL 的行：墓碑与孤立组）
	if err := d.WithContext(ctx).Exec(
		"DELETE FROM sys_i18n_text WHERE texts IS NULL").Error; err != nil {
		return fmt.Errorf("清扫统一表旧粒度残留: %w", err)
	}

	// 6. 旧索引（同名列但旧形状，必须先删）与旧列清理，再按新形状建索引
	for _, idx := range []string{"uk_i18n", "idx_i18n_batch"} {
		if err := d.dropIndexIfPresent(ctx, table, idx); err != nil {
			return err
		}
	}
	for _, col := range []string{"lang", "content"} {
		if err := d.dropColumnIfPresent(ctx, table, col); err != nil {
			return err
		}
	}
	for _, idx := range []struct {
		name, cols string
		unique     bool
	}{
		{"uk_i18n", "res_type, res_id, field, deleted_at", true},
		{"idx_i18n_batch", "res_type, field, res_id", false},
		{"idx_i18n_incomplete", "res_type, field, complete", false},
	} {
		if err := d.createIndexIfAbsent(ctx, table, idx.name, idx.cols, idx.unique); err != nil {
			return err
		}
	}
	slog.Info("统一译文表已合并为 JSON 粒度",
		"legacy_rows", len(rows), "merged_groups", mergedRows,
		"skip_default_lang", skipDefault, "skip_no_bit", skipNoBit)
	return nil
}

// mergeTextsJSON 迁移用：{lang: 译文} → JSON 串（与 dao.encodeTexts 同构；包 db 不能
// 反向 import dao，故此处独立实现——encoding/json 对 map 按键排序，两处输出一致）。
func mergeTextsJSON(texts map[string]string) string {
	if len(texts) == 0 {
		return "{}"
	}
	b, err := json.Marshal(texts)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// mergeMaskOf 迁移用：由语言集合重算 lang_mask / complete（与 dao.recompute 同构）。
func mergeMaskOf(texts map[string]string) (int64, int8) {
	var mask int64
	for l := range texts {
		if b, ok := di18n.LangBit(di18n.Lang(l)); ok {
			mask |= b
		}
	}
	var complete int8
	if full := di18n.FullMask(pi18n.Default()); full != 0 && mask&full == full {
		complete = 1
	}
	return mask, complete
}

// hasColumn 列存在性检查（迁移幂等的通用开关）。
func (d *DB) hasColumn(ctx context.Context, table, column string) (bool, error) {
	var cnt int64
	err := d.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		table, column).Scan(&cnt).Error
	return cnt > 0, err
}

// addColumnIfMissing 幂等补列。
func (d *DB) addColumnIfMissing(ctx context.Context, table, column, def string) error {
	ok, err := d.hasColumn(ctx, table, column)
	if err != nil || ok {
		return err
	}
	stmt := fmt.Sprintf("ALTER TABLE `%s` ADD COLUMN %s", table, def)
	if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("新增列 %s.%s: %w", table, column, err)
	}
	slog.Info("迁移增量列已添加", "table", table, "column", column)
	return nil
}

// dropColumnIfPresent 幂等删列。
func (d *DB) dropColumnIfPresent(ctx context.Context, table, column string) error {
	ok, err := d.hasColumn(ctx, table, column)
	if err != nil || !ok {
		return err
	}
	if err := d.WithContext(ctx).Exec(
		fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", table, column)).Error; err != nil {
		return fmt.Errorf("删除列 %s.%s: %w", table, column, err)
	}
	slog.Info("迁移已删除退役列", "table", table, "column", column)
	return nil
}

// hasIndex 索引存在性检查。
func (d *DB) hasIndex(ctx context.Context, table, index string) (bool, error) {
	var cnt int64
	err := d.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM information_schema.statistics
		  WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
		table, index).Scan(&cnt).Error
	return cnt > 0, err
}

// dropIndexIfPresent 幂等删索引。
func (d *DB) dropIndexIfPresent(ctx context.Context, table, index string) error {
	ok, err := d.hasIndex(ctx, table, index)
	if err != nil || !ok {
		return err
	}
	if err := d.WithContext(ctx).Exec(
		fmt.Sprintf("ALTER TABLE `%s` DROP INDEX `%s`", table, index)).Error; err != nil {
		return fmt.Errorf("删除索引 %s.%s: %w", table, index, err)
	}
	slog.Info("迁移已删除退役索引", "table", table, "index", index)
	return nil
}

// createIndexIfAbsent 幂等建索引。
func (d *DB) createIndexIfAbsent(ctx context.Context, table, index, cols string, unique bool) error {
	ok, err := d.hasIndex(ctx, table, index)
	if err != nil || ok {
		return err
	}
	verb := "CREATE INDEX"
	if unique {
		verb = "CREATE UNIQUE INDEX"
	}
	if err := d.WithContext(ctx).Exec(
		fmt.Sprintf("%s `%s` ON `%s` (%s)", verb, index, table, cols)).Error; err != nil {
		return fmt.Errorf("创建索引 %s.%s: %w", table, index, err)
	}
	slog.Info("迁移增量索引已创建", "table", table, "index", index)
	return nil
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

// ---------- 字典 seed（docs/系统管理-字典(sys_dict)设计.md §6） ----------

// seedDict 执行多语句 seed 文件 sys_dict_init.sql（INSERT IGNORE 幂等）。
// Migrate 主链对每个 SQL 文件整体单语句 Exec；本项目首个多语句文件在此
// 逐条执行——拆分是**引号感知**的：'...' 字符串内的分号、-- 行注释不参与拆分。
// 放在结构迁移（含 mergeI18nTextRows）全部完成之后：旧粒度窗口内的
// sys_i18n_text 没有 texts 列，seed 的合并粒度译文行必须在列就位后写入。
func (d *DB) seedDict(ctx context.Context) error {
	sql, err := conf.ReadSQL("sql/sys_dict_init.sql")
	if err != nil {
		return fmt.Errorf("读内嵌 seed sys_dict_init.sql: %w", err)
	}
	for i, stmt := range splitSQLStatements(sql) {
		if err := d.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("执行 seed sys_dict_init.sql 第 %d 条: %w", i+1, err)
		}
	}
	return nil
}

// splitSQLStatements 把多语句 SQL 源码按分号拆成单条语句（引号感知）：
// 单引号字符串（” 转义）内的分号不拆分；-- 行注释整段跳过。
// 仅供 seed 类文件使用——DDL 文件保持单语句整体 Exec，不走这里。
func splitSQLStatements(src string) []string {
	var out []string
	var buf strings.Builder
	inStr := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			buf.WriteByte(c)
			if c == '\'' {
				if i+1 < len(src) && src[i+1] == '\'' { // '' 转义
					buf.WriteByte('\'')
					i++
				} else {
					inStr = false
				}
			}
			continue
		}
		switch {
		case c == '\'':
			inStr = true
			buf.WriteByte(c)
		case c == '-' && i+1 < len(src) && src[i+1] == '-': // 行注释
			for i < len(src) && src[i] != '\n' {
				i++
			}
			buf.WriteByte('\n')
		case c == ';':
			if s := strings.TrimSpace(buf.String()); s != "" {
				out = append(out, s)
			}
			buf.Reset()
		default:
			buf.WriteByte(c)
		}
	}
	if s := strings.TrimSpace(buf.String()); s != "" {
		out = append(out, s)
	}
	return out
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
