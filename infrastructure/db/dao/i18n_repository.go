// i18n_repository —— domain/i18n.Repo 的 MySQL 实现（统一翻译表 sys_i18n_text）。
// docs《基础设施-数据文案多语言(i18n)设计.md》§5：批量读/覆盖所传语言写/同事务删。
package dao

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"dugu-hr-saas-admin/domain/authn"
	di18n "dugu-hr-saas-admin/domain/i18n"
	"dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/po"
	pi18n "dugu-hr-saas-admin/pkg/i18n"
)

// 编译期断言：满足领域端口。
var _ di18n.Repo = (*I18nRepository)(nil)

// I18nRepository 统一译文仓储。
type I18nRepository struct {
	d   *db.DB
	ids authn.IDGenerator
}

// NewI18nRepository 构造。
func NewI18nRepository(d *db.DB, ids authn.IDGenerator) *I18nRepository {
	return &I18nRepository{d: d, ids: ids}
}

// Get 列表页唯一读法：一条 SQL 批量取（r/field/lang 三常量在前、IN res_id 走 idx_i18n_batch）。
// 命不中的 id 不出现在 map，调用方回退主表默认语言列——兜底在调用方，不在这里。
func (r *I18nRepository) Get(ctx context.Context, res di18n.Resource, ids []int64, field string, lang di18n.Lang) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []po.SysI18nText
	err := r.d.WithContext(ctx).
		Select("res_id", "content").
		Where("res_type = ? AND field = ? AND lang = ? AND deleted_at = 0 AND res_id IN ?",
			res, field, lang, ids).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("i18n.get: %w", err)
	}
	for _, row := range rows {
		out[row.ResID] = row.Content
	}
	return out, nil
}

// GetMany 详情/编辑页：一次取回 id → field → lang → 译文。
// fields 空 = 白名单全量；langs 空 = 全部已注册语言（docs §5 GetMany 语义）。
func (r *I18nRepository) GetMany(ctx context.Context, res di18n.Resource, ids []int64, fields []string, langs []di18n.Lang) (map[int64]map[string]map[di18n.Lang]string, error) {
	return getManyTx(r.d.WithContext(ctx), res, ids, fields, langs)
}

// Save 只覆盖传入语言；texts[field][lang] 为空串 → 软删该语言（不能让空串盖掉兜底，
// 删除后该语言读取回退主表列）。未校验的语言/字段/译文先过白名单（30038/30039/30035）。
func (r *I18nRepository) Save(ctx context.Context, res di18n.Resource, id int64, texts map[string]map[di18n.Lang]string) error {
	if err := validateTexts(res, texts); err != nil {
		return err
	}
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return saveTx(tx, r.ids, res, id, texts)
	})
}

// DeleteAll 软删该资源全部译文（资源删除场景请走同事务的 deleteAllTx；
// 本方法供"单独清理"使用）。
func (r *I18nRepository) DeleteAll(ctx context.Context, res di18n.Resource, id int64) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteAllTx(tx, res, id)
	})
}

// ---------- 事务内小工具（同包内给业务仓储复用：读写译文并入主表事务） ----------

// getManyTx 事务内批量读。
func getManyTx(tx *gorm.DB, res di18n.Resource, ids []int64, fields []string, langs []di18n.Lang) (map[int64]map[string]map[di18n.Lang]string, error) {
	out := make(map[int64]map[string]map[di18n.Lang]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	if len(fields) == 0 {
		fields = di18n.Fields(res) // 白名单全量
	}
	if len(langs) == 0 {
		for _, l := range pi18n.Languages() {
			langs = append(langs, l)
		}
	}
	var rows []po.SysI18nText
	err := tx.
		Where("res_type = ? AND field IN ? AND lang IN ? AND deleted_at = 0 AND res_id IN ?",
			res, fields, langs, ids).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("i18n.get_many: %w", err)
	}
	for _, row := range rows {
		if out[row.ResID] == nil {
			out[row.ResID] = make(map[string]map[di18n.Lang]string)
		}
		if out[row.ResID][row.Field] == nil {
			out[row.ResID][row.Field] = make(map[di18n.Lang]string)
		}
		out[row.ResID][row.Field][di18n.Lang(row.Lang)] = row.Content
	}
	return out, nil
}

// saveTx 事务内保存（覆盖所传语言；空串=软删该语言）。
func saveTx(tx *gorm.DB, ids authn.IDGenerator, res di18n.Resource, id int64, texts map[string]map[di18n.Lang]string) error {
	for field, byLang := range texts {
		for lang, content := range byLang {
			if content == "" { // 空串 = 删除该语言的译文（回退主表列）
				if err := tx.Model(&po.SysI18nText{}).
					Where("res_type = ? AND res_id = ? AND field = ? AND lang = ? AND deleted_at = 0",
						res, id, field, lang).
					Update("deleted_at", gorm.Expr("UNIX_TIMESTAMP(NOW(3))*1000")).Error; err != nil {
					return fmt.Errorf("i18n.save.del: %w", err)
				}
				continue
			}
			row := po.SysI18nText{
				ID: ids.New(), ResType: string(res), ResID: id,
				Field: field, Lang: string(lang), Content: content,
			}
			// 命中活行（并发插入场景）→ 幂等更新译文；uk_i18n 保证不产生第二行。
			err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "res_type"}, {Name: "res_id"}, {Name: "field"},
					{Name: "lang"}, {Name: "deleted_at"},
				},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"content":    gorm.Expr("VALUES(content)"),
					"deleted_at": 0,
				}),
			}).Create(&row).Error
			if err != nil {
				return fmt.Errorf("i18n.save.upsert: %w", err)
			}
		}
	}
	return nil
}

// deleteAllTx 软删某资源全部译文（必须与主表删除同一事务，docs §5 三条约束）。
func deleteAllTx(tx *gorm.DB, res di18n.Resource, id int64) error {
	return tx.Model(&po.SysI18nText{}).
		Where("res_type = ? AND res_id = ? AND deleted_at = 0", res, id).
		Update("deleted_at", gorm.Expr("UNIX_TIMESTAMP(NOW(3))*1000")).Error
}

// validateTexts 保存前白名单/语言/内容校验（领域统一入口，30038/30039/30035）。
func validateTexts(res di18n.Resource, texts map[string]map[di18n.Lang]string) error {
	for field, byLang := range texts {
		for lang, content := range byLang {
			if err := di18n.ValidateTarget(res, field, string(lang), content); err != nil {
				return err
			}
		}
	}
	return nil
}
