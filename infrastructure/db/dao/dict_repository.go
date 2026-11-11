// dict_repository —— dict.TypeRepository / dict.ItemRepository 的 MySQL 实现
// （主表 sys_dict_type / sys_dict_item；label 译文存统一翻译表 sys_i18n_text，
// 经 I18nRepository 读写，与菜单同一机制，docs 字典设计 §7）。
package dao

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"dugu-hr-saas-admin/domain/authn"
	dict "dugu-hr-saas-admin/domain/dict"
	di18n "dugu-hr-saas-admin/domain/i18n"
	"dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/po"
	pkgi18n "dugu-hr-saas-admin/pkg/i18n"
)

// 编译期断言：满足领域端口。
var (
	_ dict.TypeRepository = (*DictTypeRepository)(nil)
	_ dict.ItemRepository = (*DictItemRepository)(nil)
)

// dictLabelField 字典项的可翻字段常量（白名单 domain/i18n.ResDictItem 内登记）。
const dictLabelField = "label"

// ---------- 类型仓储 ----------

// DictTypeRepository sys_dict_type 仓储。
type DictTypeRepository struct {
	d *db.DB
}

// NewDictTypeRepository 构造。
func NewDictTypeRepository(d *db.DB) *DictTypeRepository {
	return &DictTypeRepository{d: d}
}

// toDomainType PO → 聚合。
func toDomainType(p *po.SysDictType) *dict.Type {
	if p == nil {
		return nil
	}
	return &dict.Type{
		ID: p.ID, Code: p.Code, Name: p.Name,
		Status: dict.Status(p.Status), Remark: p.Remark,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// wrapDictTypeDup 唯一键冲突归一（uk_dict_type → ErrTypeCodeTaken）。
func wrapDictTypeDup(err error, op string) error {
	idx, dup := isDuplicate(err)
	if !dup {
		return fmt.Errorf("%s: %w", op, err)
	}
	if idx == "uk_dict_type" {
		return dict.ErrTypeCodeTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}

// Create 新建类型；code 重复（uk_dict_type）→ ErrTypeCodeTaken。
func (r *DictTypeRepository) Create(ctx context.Context, t *dict.Type) error {
	p := &po.SysDictType{
		ID: t.ID, Code: t.Code, Name: t.Name,
		Status: int8(t.Status), Remark: t.Remark,
	}
	if err := r.d.WithContext(ctx).Create(p).Error; err != nil {
		return wrapDictTypeDup(err, "dict_type.create")
	}
	t.CreatedAt, t.UpdatedAt = p.CreatedAt, p.UpdatedAt
	return nil
}

// ByID 未命中 (nil, nil)。
func (r *DictTypeRepository) ByID(ctx context.Context, id int64) (*dict.Type, error) {
	var p po.SysDictType
	err := r.d.WithContext(ctx).First(&p, "id = ? AND deleted_at = 0", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dict_type.by_id: %w", err)
	}
	return toDomainType(&p), nil
}

// Save 内容字段更新（code/name/status/remark）；code 重复 → ErrTypeCodeTaken。
func (r *DictTypeRepository) Save(ctx context.Context, t *dict.Type) error {
	err := r.d.WithContext(ctx).Model(&po.SysDictType{}).
		Where("id = ? AND deleted_at = 0", t.ID).
		Updates(map[string]interface{}{
			"code": t.Code, "name": t.Name, "status": int8(t.Status), "remark": t.Remark,
		}).Error
	if err != nil {
		return wrapDictTypeDup(err, "dict_type.save")
	}
	return nil
}

// Delete 软删（type.name 不做多语言，无译文行可删；项数>0 由应用层先行拒绝）。
func (r *DictTypeRepository) Delete(ctx context.Context, id int64) error {
	return r.d.WithContext(ctx).Model(&po.SysDictType{}).
		Where("id = ? AND deleted_at = 0", id).
		Update("deleted_at", gorm.Expr("UNIX_TIMESTAMP(NOW(3))*1000")).Error
}

// List 分页列表：code 非空 = 编码模糊过滤；status 过滤；按 code 升序（稳定顺序）。
// COUNT + Limit/Offset 两阶段，normalizePage 与 sys-users 列表同一惯例。
func (r *DictTypeRepository) List(ctx context.Context, q dict.ListTypesQuery) ([]dict.Type, int64, error) {
	page, size := normalizePage(q.Page, q.Size)
	base := r.d.WithContext(ctx).Model(&po.SysDictType{}).Where("deleted_at = 0")
	if q.Code != "" {
		base = base.Where("code LIKE ?", "%"+q.Code+"%")
	}
	if q.Status != nil {
		base = base.Where("status = ?", int8(*q.Status))
	}
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("dict_type.list.count: %w", err)
	}
	var rows []po.SysDictType
	if err := base.
		Order("code ASC").Limit(size).Offset((page - 1) * size).
		Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("dict_type.list.find: %w", err)
	}
	out := make([]dict.Type, 0, len(rows))
	for i := range rows {
		out = append(out, *toDomainType(&rows[i]))
	}
	return out, total, nil
}

// CountItems 类型下未删除项数（删除前置校验，docs §5 不做级联软删）。
func (r *DictTypeRepository) CountItems(ctx context.Context, typeID int64) (int64, error) {
	var cnt int64
	err := r.d.WithContext(ctx).Model(&po.SysDictItem{}).
		Where("type_id = ? AND deleted_at = 0", typeID).Count(&cnt).Error
	if err != nil {
		return 0, fmt.Errorf("dict_type.count_items: %w", err)
	}
	return cnt, nil
}

// ByCode 未命中 (nil, nil)（应用层按 typeCode 定位类型用）。
func (r *DictTypeRepository) ByCode(ctx context.Context, code string) (*dict.Type, error) {
	var p po.SysDictType
	err := r.d.WithContext(ctx).First(&p, "code = ? AND deleted_at = 0", code).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dict_type.by_code: %w", err)
	}
	return toDomainType(&p), nil
}

// ---------- 项仓储 ----------

// DictItemRepository sys_dict_item 仓储（labels 走统一翻译表）。
type DictItemRepository struct {
	d   *db.DB
	ids authn.IDGenerator
	t   *I18nRepository
}

// NewDictItemRepository 构造（统一翻译仓储内部装配）。
func NewDictItemRepository(d *db.DB, ids authn.IDGenerator) *DictItemRepository {
	return &DictItemRepository{d: d, ids: ids, t: NewI18nRepository(d, ids)}
}

// labelTexts 把 labels（lang→译文）转成统一表 Save 的 {field:{lang:content}} 形状。
// **默认语言跳过**（§7.4：主表 label 已是默认语言值，译文表再存一份就是两份真相）。
func labelTexts(labels map[string]string) map[string]map[di18n.Lang]string {
	if len(labels) == 0 {
		return nil
	}
	langs := make(map[di18n.Lang]string, len(labels))
	for l, v := range labels {
		if pkgi18n.Lang(l) == pkgi18n.Default() {
			continue
		}
		langs[di18n.Lang(l)] = v
	}
	if len(langs) == 0 {
		return nil
	}
	return map[string]map[di18n.Lang]string{dictLabelField: langs}
}

// toDomainItem PO → 聚合（不含 labels；labels 由调用方批量补）。
func toDomainItem(p *po.SysDictItem) *dict.Item {
	if p == nil {
		return nil
	}
	return &dict.Item{
		ID: p.ID, TypeID: p.TypeID, Value: p.Value, Label: p.Label,
		Sort: p.Sort, Status: dict.Status(p.Status), Remark: p.Remark,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// wrapDictItemDup 唯一键冲突归一（uk_dict_item → ErrValueTaken）。
func wrapDictItemDup(err error, op string) error {
	idx, dup := isDuplicate(err)
	if !dup {
		return fmt.Errorf("%s: %w", op, err)
	}
	if idx == "uk_dict_item" {
		return dict.ErrValueTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}

// Create 事务：主表行 + 统一表空译文行（与菜单"创建必插空行"同一不变式，
// docs 基础设施-i18n设计 v1.13/v1.14 用户裁定语义对齐）+ labels 非默认语言部分。
func (r *DictItemRepository) Create(ctx context.Context, i *dict.Item) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p := &po.SysDictItem{
			ID: i.ID, TypeID: i.TypeID, Label: i.Label, Value: i.Value,
			Sort: i.Sort, Status: int8(i.Status), Remark: i.Remark,
		}
		if err := tx.Create(p).Error; err != nil {
			return wrapDictItemDup(err, "dict_item.create")
		}
		if err := ensureRowTx(tx, r.ids, di18n.ResDictItem, i.ID, dictLabelField); err != nil {
			return err
		}
		if texts := labelTexts(i.Labels); texts != nil {
			if err := saveTx(tx, r.ids, di18n.ResDictItem, i.ID, texts); err != nil {
				return err
			}
		}
		i.CreatedAt, i.UpdatedAt = p.CreatedAt, p.UpdatedAt
		return nil
	})
}

// ByID 主表 + labels 全集（编辑页回填）；未命中 (nil, nil)。
func (r *DictItemRepository) ByID(ctx context.Context, id int64) (*dict.Item, error) {
	var p po.SysDictItem
	err := r.d.WithContext(ctx).First(&p, "id = ? AND deleted_at = 0", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dict_item.by_id: %w", err)
	}
	i := toDomainItem(&p)
	labels, err := r.LabelsByIDs(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	i.Labels = labels[id]
	return i, nil
}

// Save 主表内容字段更新（label/value/sort/status/remark）；value 重复 → ErrValueTaken。
func (r *DictItemRepository) Save(ctx context.Context, i *dict.Item) error {
	err := r.d.WithContext(ctx).Model(&po.SysDictItem{}).
		Where("id = ? AND deleted_at = 0", i.ID).
		Updates(map[string]interface{}{
			"label": i.Label, "value": i.Value, "sort": i.Sort,
			"status": int8(i.Status), "remark": i.Remark,
		}).Error
	if err != nil {
		return wrapDictItemDup(err, "dict_item.save")
	}
	return nil
}

// Delete 软删主表 + 统一翻译表对应译文行（同一事务，docs 基础设施-i18n设计 §5）。
func (r *DictItemRepository) Delete(ctx context.Context, id int64) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&po.SysDictItem{}).
			Where("id = ? AND deleted_at = 0", id).
			Update("deleted_at", gorm.Expr("UNIX_TIMESTAMP(NOW(3))*1000")).Error; err != nil {
			return fmt.Errorf("dict_item.delete: %w", err)
		}
		if err := deleteAllTx(tx, di18n.ResDictItem, id); err != nil {
			return fmt.Errorf("dict_item.delete_i18n: %w", err)
		}
		return nil
	})
}

// ListByType 类型下项列表：sort 升序同 sort 按 id（稳定顺序，docs §2）；
// withDisabled=false 只返回启用项；lang 非空时批量取该语言译文（缺译回退主表 label）。
func (r *DictItemRepository) ListByType(ctx context.Context, typeID int64, withDisabled bool, lang pkgi18n.Lang) ([]dict.Item, error) {
	q := r.d.WithContext(ctx).Where("type_id = ? AND deleted_at = 0", typeID)
	if !withDisabled {
		q = q.Where("status = ?", int8(dict.StatusOn))
	}
	var rows []po.SysDictItem
	if err := q.Order("sort ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("dict_item.list_by_type: %w", err)
	}
	out := make([]dict.Item, 0, len(rows))
	for i := range rows {
		out = append(out, *toDomainItem(&rows[i]))
	}
	if lang == "" || len(out) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(out))
	for i := range out {
		ids = append(ids, out[i].ID)
	}
	got, err := r.t.Get(ctx, di18n.ResDictItem, ids, dictLabelField, di18n.Lang(lang))
	if err != nil {
		return nil, err
	}
	for i := range out { // 命中译文挂 Labels 供 DisplayLabel 解析；命不中回退主表 label
		if v, ok := got[out[i].ID]; ok {
			out[i].Labels = map[string]string{string(lang): v}
		}
	}
	return out, nil
}

// UpsertLabels 覆盖所传语言（未传语言不动）；空串 = 删除该语言译文（统一表 Save 语义）。
func (r *DictItemRepository) UpsertLabels(ctx context.Context, itemID int64, labels map[string]string) error {
	if len(labels) == 0 {
		return nil
	}
	return r.t.Save(ctx, di18n.ResDictItem, itemID, labelTexts(labels))
}

// LabelsByIDs 批量取全部语言的 label 译文（走统一翻译表 GetMany；详情/编辑页回填用）。
// **默认语言的行剔除**（§7.4：默认语言直接取主表 label，与菜单 names 同一读规则）。
func (r *DictItemRepository) LabelsByIDs(ctx context.Context, ids []int64) (map[int64]map[string]string, error) {
	many, err := r.t.GetMany(ctx, di18n.ResDictItem, ids, []string{dictLabelField}, nil)
	if err != nil {
		return nil, err
	}
	def := pkgi18n.Default()
	out := make(map[int64]map[string]string, len(many))
	for id, byField := range many {
		byLang := byField[dictLabelField]
		if len(byLang) == 0 {
			continue
		}
		labels := make(map[string]string, len(byLang))
		for l, v := range byLang {
			if pkgi18n.Lang(l) == def {
				continue // 默认语言排除（读只回真正"译文"）
			}
			labels[string(l)] = v
		}
		if len(labels) > 0 {
			out[id] = labels
		}
	}
	return out, nil
}
