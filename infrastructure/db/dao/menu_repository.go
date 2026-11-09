// menu_repository —— menu.Repository 的 MySQL 实现（主表 sys_menu + 译文子表 sys_menu_i18n）。
package dao

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/menu"
	"dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/po"
)

// 编译期断言：满足领域端口。
var _ menu.Repository = (*MenuRepository)(nil)

// MenuRepository sys_menu 仓储。
type MenuRepository struct {
	d   *db.DB
	ids authn.IDGenerator
}

// NewMenuRepository 构造。
func NewMenuRepository(d *db.DB, ids authn.IDGenerator) *MenuRepository {
	return &MenuRepository{d: d, ids: ids}
}

// toDomainMenu PO → 聚合（不含 names；names 由 NamesByIDs 批量补）。
func toDomainMenu(p *po.SysMenu) *menu.Menu {
	if p == nil {
		return nil
	}
	return &menu.Menu{
		ID: p.ID, ParentID: p.ParentID, Name: p.Name,
		Type: menu.Type(p.Type),
		Path: p.Path, Component: p.Component, Icon: p.Icon,
		Sort: p.Sort, Visible: p.Visible, Status: menu.Status(p.Status),
		ActionCode: p.ActionCode, Remark: p.Remark,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// Create 主表 + names 子表同事务新建；唯一冲突归类（uk_action → ErrActionCodeTaken）。
func (r *MenuRepository) Create(ctx context.Context, m *menu.Menu) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p := &po.SysMenu{
			ID: m.ID, ParentID: m.ParentID, Name: m.Name,
			Type: int(m.Type),
			Path: m.Path, Component: m.Component, Icon: m.Icon,
			Sort: m.Sort, Visible: m.Visible, Status: int(m.Status),
			ActionCode: m.ActionCode, Remark: m.Remark,
		}
		if err := tx.Create(p).Error; err != nil {
			return wrapMenuDup(err, "menu.create")
		}
		if err := upsertI18n(tx, r.ids, m.ID, m.Names); err != nil {
			return err
		}
		m.CreatedAt, m.UpdatedAt = p.CreatedAt, p.UpdatedAt
		return nil
	})
}

// ByID 主表 + names 全集，未命中 (nil,nil)。
func (r *MenuRepository) ByID(ctx context.Context, mid int64) (*menu.Menu, error) {
	var p po.SysMenu
	err := r.d.WithContext(ctx).First(&p, "id = ? AND deleted_at = 0", mid).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("menu.by_id: %w", err)
	}
	m := toDomainMenu(&p)
	names, err := r.NamesByIDs(ctx, []int64{mid})
	if err != nil {
		return nil, err
	}
	m.Names = names[mid]
	return m, nil
}

// Save 主表内容字段更新。
func (r *MenuRepository) Save(ctx context.Context, m *menu.Menu) error {
	err := r.d.WithContext(ctx).Model(&po.SysMenu{}).
		Where("id = ? AND deleted_at = 0", m.ID).
		Updates(map[string]interface{}{
			"name": m.Name, "type": int(m.Type),
			"path": m.Path, "component": m.Component, "icon": m.Icon,
			"sort": m.Sort, "visible": m.Visible, "status": int(m.Status),
			"action_code": m.ActionCode, "remark": m.Remark,
		}).Error
	if err != nil {
		return wrapMenuDup(err, "menu.save")
	}
	return nil
}

// UpdateSort 仅排序（管理台“保存排序”）。
func (r *MenuRepository) UpdateSort(ctx context.Context, mid int64, sortV int) error {
	return r.d.WithContext(ctx).Model(&po.SysMenu{}).
		Where("id = ? AND deleted_at = 0", mid).
		Update("sort", sortV).Error
}

// Move 结构变更：换父。
func (r *MenuRepository) Move(ctx context.Context, mid, parentID int64) error {
	return r.d.WithContext(ctx).Model(&po.SysMenu{}).
		Where("id = ? AND deleted_at = 0", mid).
		Update("parent_id", parentID).Error
}

// UpsertNames 覆盖所传语言（未传不动）：uk_menu_i18n 冲突 → 活行改名。
func (r *MenuRepository) UpsertNames(ctx context.Context, mid int64, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return upsertI18n(tx, r.ids, mid, names)
	})
}

// Delete 软删主表 + 译文子表（同事务）。
func (r *MenuRepository) Delete(ctx context.Context, mid int64) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stmt := "UNIX_TIMESTAMP(NOW(3))*1000"
		if err := tx.Model(&po.SysMenu{}).
			Where("id = ? AND deleted_at = 0", mid).
			Update("deleted_at", gorm.Expr(stmt)).Error; err != nil {
			return fmt.Errorf("menu.delete: %w", err)
		}
		if err := tx.Model(&po.SysMenuI18n{}).
			Where("menu_id = ? AND deleted_at = 0", mid).
			Update("deleted_at", gorm.Expr(stmt)).Error; err != nil {
			return fmt.Errorf("menu.delete_i18n: %w", err)
		}
		return nil
	})
}

// HasChildren 是否存在未删除子节点。
func (r *MenuRepository) HasChildren(ctx context.Context, mid int64) (bool, error) {
	var cnt int64
	err := r.d.WithContext(ctx).Model(&po.SysMenu{}).
		Where("parent_id = ? AND deleted_at = 0", mid).Count(&cnt).Error
	if err != nil {
		return false, fmt.Errorf("menu.has_children: %w", err)
	}
	return cnt > 0, nil
}

// ListAll 全量未删除节点（sort, id 排序——树组装时同父子孙相对有序）。
func (r *MenuRepository) ListAll(ctx context.Context) ([]menu.Menu, error) {
	var items []po.SysMenu
	err := r.d.WithContext(ctx).
		Where("deleted_at = 0").
		Order("sort ASC, id ASC").Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("menu.list_all: %w", err)
	}
	out := make([]menu.Menu, 0, len(items))
	for i := range items {
		out = append(out, *toDomainMenu(&items[i]))
	}
	return out, nil
}

// NamesByIDs 批量取译文。
func (r *MenuRepository) NamesByIDs(ctx context.Context, ids []int64) (map[int64]map[string]string, error) {
	out := make(map[int64]map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []po.SysMenuI18n
	err := r.d.WithContext(ctx).
		Where("deleted_at = 0 AND menu_id IN ?", ids).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("menu.names_by_ids: %w", err)
	}
	for _, row := range rows {
		if out[row.MenuID] == nil {
			out[row.MenuID] = make(map[string]string)
		}
		out[row.MenuID][row.Lang] = row.Name
	}
	return out, nil
}

// ---------- 事务内 i18n 小工具 ----------

// upsertI18n 对已归一化 langs 做 upsert：命中 uk_menu_i18n 活行 → 改名；否则插入。
func upsertI18n(tx *gorm.DB, ids authn.IDGenerator, menuID int64, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}
	rows := make([]po.SysMenuI18n, 0, len(names))
	for l, v := range names {
		rows = append(rows, po.SysMenuI18n{ID: ids.New(), MenuID: menuID, Lang: l, Name: v})
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "menu_id"}, {Name: "lang"}},
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	}).Create(&rows).Error
}

// wrapMenuDup 唯一键冲突归一。
func wrapMenuDup(err error, op string) error {
	idx, dup := isDuplicate(err)
	if !dup {
		return fmt.Errorf("%s: %w", op, err)
	}
	if idx == "uk_action" {
		return menu.ErrActionCodeTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}
