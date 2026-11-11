// i18n_repository —— domain/i18n.Repo 的 MySQL 实现（统一翻译表 sys_i18n_text）。
// docs《基础设施-数据文案多语言(i18n)设计.md》§5：批量读/覆盖所传语言写/同事务删。
// v1.5 合并粒度：一行 = 资源 × 字段，全部语言进 texts JSON + lang_mask/complete（§2.4）；
// 并发编辑同一字段由 FOR UPDATE 行锁串行化（JSON 读-改-写必须持锁，否则丢更新）。
package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

// ---------- texts JSON 编解码与 lang_mask/complete 重算 ----------

// decodeTexts texts JSON → {lang: 译文}。空串/解析失败按空对象处理
// （列有 JSON 校验，理论不会失败；防御损坏行不让读路径 500）。
func decodeTexts(raw string) map[di18n.Lang]string {
	m := make(map[di18n.Lang]string, 4)
	if raw == "" {
		return m
	}
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}

// encodeTexts {lang: 译文} → texts JSON。encoding/json 对 map 按键排序输出，
// 落库形态确定（同一内容恒同字节，便于 diff 与测试断言）。
func encodeTexts(m map[di18n.Lang]string) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil { // 键是归一化语言标签、值已过 30039 校验，不可达；防御兜底
		return "{}"
	}
	return string(b)
}

// recompute 由 texts 重算 lang_mask / complete（写入路径唯一入口，禁止手工维护位）。
func recompute(texts map[di18n.Lang]string) (mask int64, complete int8) {
	for l := range texts {
		if b, ok := di18n.LangBit(l); ok {
			mask |= b
		}
	}
	if full := di18n.FullMask(pi18n.Default()); full != 0 && mask&full == full {
		complete = 1
	}
	return mask, complete
}

// ---------- Repo 实现 ----------

// Get 列表页唯一读法：一条 SQL 批量取（r/field 常量在前、IN res_id 走 idx_i18n_batch），
// 语言维度在 Go 侧从 JSON 取键。命不中的 id 不出现在 map，调用方回退主表默认语言列。
func (r *I18nRepository) Get(ctx context.Context, res di18n.Resource, ids []int64, field string, lang di18n.Lang) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []po.SysI18nText
	err := r.d.WithContext(ctx).
		Select("res_id", "texts").
		Where("res_type = ? AND field = ? AND deleted_at = 0 AND res_id IN ?",
			res, field, ids).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("i18n.get: %w", err)
	}
	for _, row := range rows {
		if v, ok := decodeTexts(row.Texts)[lang]; ok {
			out[row.ResID] = v
		}
	}
	return out, nil
}

// GetMany 详情/编辑页：一次取回 id → field → lang → 译文（JSON 一次读出全部语言，
// langs 维度在 Go 侧过滤）。fields/langs 空 = 白名单/注册语言全量（docs §5 GetMany 语义）。
func (r *I18nRepository) GetMany(ctx context.Context, res di18n.Resource, ids []int64, fields []string, langs []di18n.Lang) (map[int64]map[string]map[di18n.Lang]string, error) {
	return getManyTx(r.d.WithContext(ctx), res, ids, fields, langs)
}

// Save 只覆盖传入语言；texts[field][lang] 为空串 → 从 JSON 移除该语言（不能让空串
// 盖掉兜底，移除后该语言读取回退主表列）。未校验的语言/字段/译文先过白名单（30038/30039/30035）。
func (r *I18nRepository) Save(ctx context.Context, res di18n.Resource, id int64, texts map[string]map[di18n.Lang]string) error {
	if err := validateTexts(res, texts); err != nil {
		return err
	}
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return saveTx(tx, r.ids, res, id, texts)
	})
}

// DeleteAll 软删该资源全部译文行（合并粒度下墓碑 = 资源 × 字段整行；
// 资源删除场景请走同事务的 deleteAllTx，本方法供"单独清理"使用）。
func (r *I18nRepository) DeleteAll(ctx context.Context, res di18n.Resource, id int64) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteAllTx(tx, res, id)
	})
}

// BatchDelete 批量软删（§7.7 三档第 2 档）：单事务全成功或全失败。
// 合并粒度下"删某语言"= 对应行 JSON 移除键 + 位清零（行保留，非整行墓碑）；
// 幂等：目标语言不存在计 0，不报错。返回值为实际移除的语言数。
func (r *I18nRepository) BatchDelete(ctx context.Context, keys []di18n.DelKey) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	var total int64
	err := r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		group := map[di18n.Resource]map[string]map[string][]int64{} // res → field → lang → ids
		for _, k := range keys {
			if group[k.Resource] == nil {
				group[k.Resource] = map[string]map[string][]int64{}
			}
			if group[k.Resource][k.Field] == nil {
				group[k.Resource][k.Field] = map[string][]int64{}
			}
			group[k.Resource][k.Field][string(k.Lang)] = append(group[k.Resource][k.Field][string(k.Lang)], k.ID)
		}
		for res, byField := range group {
			for field, byLang := range byField {
				for lang, ids := range byLang {
					n, err := removeLangTx(tx, res, field, lang, ids)
					if err != nil {
						return err
					}
					total += n
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("i18n.batch_delete: %w", err)
	}
	return total, nil
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
	var rows []po.SysI18nText
	err := tx.
		Where("res_type = ? AND field IN ? AND deleted_at = 0 AND res_id IN ?",
			res, fields, ids).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("i18n.get_many: %w", err)
	}
	langSet := map[di18n.Lang]bool{}
	if len(langs) == 0 { // 空 = 全部已注册语言（JSON 里本就只有这些）
		for _, l := range pi18n.Languages() {
			langSet[l] = true
		}
	} else {
		for _, l := range langs {
			langSet[l] = true
		}
	}
	for _, row := range rows {
		for l, v := range decodeTexts(row.Texts) {
			if !langSet[l] {
				continue
			}
			if out[row.ResID] == nil {
				out[row.ResID] = make(map[string]map[di18n.Lang]string)
			}
			if out[row.ResID][row.Field] == nil {
				out[row.ResID][row.Field] = make(map[di18n.Lang]string)
			}
			out[row.ResID][row.Field][l] = v
		}
	}
	return out, nil
}

// saveTx 事务内保存（覆盖所传语言；空串=移除该语言）。
func saveTx(tx *gorm.DB, ids authn.IDGenerator, res di18n.Resource, id int64, texts map[string]map[di18n.Lang]string) error {
	for field, byLang := range texts {
		if err := mergeFieldTx(tx, ids, res, id, field, byLang); err != nil {
			return err
		}
	}
	return nil
}

// mergeAttempts 并发冲突重试上限（首存撞唯一键 / gap 锁死锁）。
const mergeAttempts = 3

// mergeFieldTx 合并保存某资源某字段的全部语言变更。
// 并发正确性：命中活行时先 FOR UPDATE 行锁，再读-改-写 JSON——两个事务同时编辑
// 同一资源同一字段时串行执行，后到者基于先到者的结果合并，不丢更新。
// 首存竞态（两边都查不到行）：一方插入成功，另一方撞 uk_i18n → 重读合并（重试收敛）。
func mergeFieldTx(tx *gorm.DB, ids authn.IDGenerator, res di18n.Resource, id int64, field string, byLang map[di18n.Lang]string) error {
	var lastErr error
	for attempt := 0; attempt < mergeAttempts; attempt++ {
		var row po.SysI18nText
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("res_type = ? AND res_id = ? AND field = ? AND deleted_at = 0", res, id, field).
			First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			merged := make(map[di18n.Lang]string, len(byLang))
			for l, v := range byLang { // 删除语义对不存在的行是 no-op
				if v != "" {
					merged[l] = v
				}
			}
			if len(merged) == 0 {
				return nil // 没有可存内容，不建空行
			}
			mask, complete := recompute(merged)
			ins := po.SysI18nText{
				ID: ids.New(), ResType: string(res), ResID: id, Field: field,
				Texts: encodeTexts(merged), LangMask: mask, Complete: complete,
			}
			if err := tx.Create(&ins).Error; err != nil {
				if isConflictErr(err) || isDeadlockErr(err) { // 并发首存 → 重读合并
					lastErr = err
					continue
				}
				return fmt.Errorf("i18n.save.insert: %w", err)
			}
			return nil
		}
		if err != nil {
			if isDeadlockErr(err) { // FOR UPDATE gap 锁死锁 → 重试
				lastErr = err
				continue
			}
			return fmt.Errorf("i18n.save.select: %w", err)
		}
		// 命中活行：行锁已持有，合并写回
		merged := decodeTexts(row.Texts)
		for l, v := range byLang {
			if v == "" {
				delete(merged, l)
			} else {
				merged[l] = v
			}
		}
		mask, complete := recompute(merged)
		if err := tx.Model(&po.SysI18nText{}).Where("id = ?", row.ID).
			Updates(map[string]interface{}{
				"texts": encodeTexts(merged), "lang_mask": mask, "complete": complete,
			}).Error; err != nil {
			if isDeadlockErr(err) {
				lastErr = err
				continue
			}
			return fmt.Errorf("i18n.save.update: %w", err)
		}
		return nil
	}
	return fmt.Errorf("i18n.save: 并发冲突重试 %d 次未成功: %w", mergeAttempts, lastErr)
}

// removeLangTx 从一组行的 texts JSON 中移除某语言（BatchDelete / 删除语义共用）。
// 返回实际移除的语言数（行无此语言 = 幂等计 0）。
func removeLangTx(tx *gorm.DB, res di18n.Resource, field string, lang string, ids []int64) (int64, error) {
	var rows []po.SysI18nText
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "texts").
		Where("res_type = ? AND field = ? AND deleted_at = 0 AND res_id IN ?", res, field, ids).
		Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("i18n.remove_lang.select: %w", err)
	}
	var n int64
	for _, row := range rows {
		m := decodeTexts(row.Texts)
		if _, ok := m[di18n.Lang(lang)]; !ok {
			continue
		}
		delete(m, di18n.Lang(lang))
		mask, complete := recompute(m)
		if err := tx.Model(&po.SysI18nText{}).Where("id = ?", row.ID).
			Updates(map[string]interface{}{
				"texts": encodeTexts(m), "lang_mask": mask, "complete": complete,
			}).Error; err != nil {
			return n, fmt.Errorf("i18n.remove_lang.update: %w", err)
		}
		n++
	}
	return n, nil
}

// deleteAllTx 软删某资源全部译文行（必须与主表删除同一事务，docs §5 三条约束）。
func deleteAllTx(tx *gorm.DB, res di18n.Resource, id int64) error {
	return tx.Model(&po.SysI18nText{}).
		Where("res_type = ? AND res_id = ? AND deleted_at = 0", res, id).
		Update("deleted_at", gorm.Expr("UNIX_TIMESTAMP(NOW(3))*1000")).Error
}

// isConflictErr MySQL 唯一键冲突（1062）。gorm.ErrDuplicatedKey 需翻译器开启，
// 直接按错误文本判定（两张网都收，避免依赖驱动错误包装形态）。
func isConflictErr(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate entry")
}

// isDeadlockErr InnoDB 死锁（1213，会被回滚一方）与锁等待超时（1205）。
func isDeadlockErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadlock") || strings.Contains(msg, "lock wait timeout")
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
