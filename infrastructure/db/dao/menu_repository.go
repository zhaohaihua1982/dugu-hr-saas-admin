// menu_repository —— menu.Repository 的 MySQL 实现（主表 sys_menu；
// 名称译文存统一翻译表 sys_i18n_text，经 I18nRepository 读写，v0.6 起不再用 sys_menu_i18n）。
package dao

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"dugu-hr-saas-admin/domain/authn"
	di18n "dugu-hr-saas-admin/domain/i18n"
	"dugu-hr-saas-admin/domain/menu"
	"dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/po"
	pkgi18n "dugu-hr-saas-admin/pkg/i18n"
)

// 编译期断言：满足领域端口。
var _ menu.Repository = (*MenuRepository)(nil)

// MenuRepository sys_menu 仓储。
type MenuRepository struct {
	d   *db.DB
	ids authn.IDGenerator
	t   *I18nRepository // 名称译文走统一翻译表（docs 基础设施-i18n设计 §8：切换读/写路径）
}

// NewMenuRepository 构造（统一翻译仓储内部装配，调用方签名不变）。
func NewMenuRepository(d *db.DB, ids authn.IDGenerator) *MenuRepository {
	return &MenuRepository{d: d, ids: ids, t: NewI18nRepository(d, ids)}
}

// nameTexts 把菜单的 names（lang→名）转成统一表 Save 的 {field:{lang:content}} 形状。
// **默认语言跳过**（§7.4：主表 name 已是默认语言值，译文表再存一份就是两份真相）。
func nameTexts(names map[string]string) map[string]map[di18n.Lang]string {
	if len(names) == 0 {
		return nil
	}
	langs := make(map[di18n.Lang]string, len(names))
	for l, v := range names {
		if pkgi18n.Lang(l) == pkgi18n.Default() {
			continue
		}
		langs[di18n.Lang(l)] = v
	}
	if len(langs) == 0 {
		return nil
	}
	return map[string]map[di18n.Lang]string{menuNameField: langs}
}

// menuNameField 菜单的可翻字段常量（白名单 domain/i18n.ResMenu 内登记）。
const menuNameField = "name"

// toDomainMenu PO → 聚合（不含 names；names 由 NamesByIDs 批量补）。
func toDomainMenu(p *po.SysMenu) *menu.Menu {
	if p == nil {
		return nil
	}
	return &menu.Menu{
		ID: p.ID, ParentID: p.ParentID, Name: p.Name,
		Type: menu.Type(p.Type),
		Path: p.Path, Component: p.Component, Icon: p.Icon,
		Sort: p.Sort, IDPath: p.IDPath, Visible: p.Visible, Status: menu.Status(p.Status),
		Code: p.Code, Remark: p.Remark,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// Create 主表 + names 子表同事务新建；同级重复归类（30037，wrapMenuDup）。
// id_path 在事务内按父节点现值计算（根=自身；子="父id_path/自身"）。
func (r *MenuRepository) Create(ctx context.Context, m *menu.Menu) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var parentPath string
		if m.ParentID != 0 {
			var pp po.SysMenu
			if err := tx.Select("id_path").
				First(&pp, "id = ? AND deleted_at = 0", m.ParentID).Error; err != nil {
				return fmt.Errorf("menu.create.parent_path: %w", err)
			}
			parentPath = pp.IDPath
		}
		m.IDPath = joinIDPath(parentPath, m.ID)
		p := &po.SysMenu{
			ID: m.ID, ParentID: m.ParentID, Name: m.Name,
			Type: int(m.Type),
			Path: m.Path, Component: m.Component, Icon: m.Icon,
			Sort: m.Sort, Visible: m.Visible, Status: int(m.Status),
			IDPath: m.IDPath,
			Code:   m.Code, Remark: m.Remark,
		}
		if err := tx.Create(p).Error; err != nil {
			return wrapMenuDup(err, "menu.create")
		}
		// 菜单创建必插一条空译文行（用户裁定 v1.13）：每菜单在统一表恒有一行；
		// 随后 names 非默认语言部分 upsert 到该行上（saveTx 命中活行走合并更新）。
		if err := ensureRowTx(tx, r.ids, di18n.ResMenu, m.ID, menuNameField); err != nil {
			return err
		}
		if texts := nameTexts(m.Names); texts != nil {
			if err := saveTx(tx, r.ids, di18n.ResMenu, m.ID, texts); err != nil {
				return err
			}
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
			"code": m.Code, "remark": m.Remark,
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

// Move 结构变更：换父并重算本节点及其子树的 id_path（同事务）。
// 重算规则：本节点 id_path = 新父 id_path/自身；子树 = 新前缀 + 旧路径自前缀之后的部分。
func (r *MenuRepository) Move(ctx context.Context, mid, parentID int64) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var self po.SysMenu
		if err := tx.First(&self, "id = ? AND deleted_at = 0", mid).Error; err != nil {
			return fmt.Errorf("menu.move.self: %w", err)
		}
		var parentPath string
		if parentID != 0 {
			var pp po.SysMenu
			if err := tx.Select("id_path").
				First(&pp, "id = ? AND deleted_at = 0", parentID).Error; err != nil {
				return fmt.Errorf("menu.move.parent_path: %w", err)
			}
			parentPath = pp.IDPath
		}
		newPath := joinIDPath(parentPath, mid)
		oldPath := self.IDPath
		if newPath == oldPath && parentID == self.ParentID {
			return nil
		}
		if err := tx.Model(&po.SysMenu{}).
			Where("id = ?", mid).
			Updates(map[string]interface{}{"parent_id": parentID, "id_path": newPath}).Error; err != nil {
			return fmt.Errorf("menu.move.self_path: %w", err)
		}
		// 子树整体平移：SUBSTRING 起点 = len(oldPath)+2（1-based，越过“旧前缀+分隔斜杠”）。
		err := tx.Exec(
			"UPDATE sys_menu SET id_path = CONCAT(?, '/', SUBSTRING(id_path, ?)) "+
				"WHERE deleted_at = 0 AND id <> ? AND id_path LIKE CONCAT(?, '/%')",
			newPath, len(oldPath)+2, mid, oldPath).Error
		if err != nil {
			return fmt.Errorf("menu.move.subtree: %w", err)
		}
		return nil
	})
}

// joinIDPath 拼接父子链（根节点：父路径为空 → 只剩自身 id）。
func joinIDPath(parentPath string, id int64) string {
	if parentPath == "" {
		return strconv.FormatInt(id, 10)
	}
	return parentPath + "/" + strconv.FormatInt(id, 10)
}

// UpsertNames 覆盖所传语言（未传不动）；空串 = 软删该语言译文（统一表 Save 语义）。
func (r *MenuRepository) UpsertNames(ctx context.Context, mid int64, names map[string]string) error {
	if len(names) == 0 {
		return nil
	}
	return r.t.Save(ctx, di18n.ResMenu, mid, nameTexts(names))
}

// Delete 软删主表 + 统一翻译表对应译文（同一事务，docs 基础设施-i18n设计 §5）。
func (r *MenuRepository) Delete(ctx context.Context, mid int64) error {
	return r.d.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stmt := "UNIX_TIMESTAMP(NOW(3))*1000"
		if err := tx.Model(&po.SysMenu{}).
			Where("id = ? AND deleted_at = 0", mid).
			Update("deleted_at", gorm.Expr(stmt)).Error; err != nil {
			return fmt.Errorf("menu.delete: %w", err)
		}
		if err := deleteAllTx(tx, di18n.ResMenu, mid); err != nil {
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

// NamesByIDs 批量取全部语言的 name 译文（走统一翻译表 GetMany；详情/编辑页回填用）。
// **默认语言的行剔除**（§7.4：默认语言直接取主表 name，译文表遗留 zh-CN 历史行不回传）。
func (r *MenuRepository) NamesByIDs(ctx context.Context, ids []int64) (map[int64]map[string]string, error) {
	many, err := r.t.GetMany(ctx, di18n.ResMenu, ids, []string{menuNameField}, nil)
	if err != nil {
		return nil, err
	}
	def := pkgi18n.Default()
	out := make(map[int64]map[string]string, len(many))
	for id, byField := range many {
		byLang := byField[menuNameField]
		if len(byLang) == 0 {
			continue
		}
		names := make(map[string]string, len(byLang))
		for l, v := range byLang {
			if pkgi18n.Lang(l) == def {
				continue // 默认语言排除（读只回真正"译文"）
			}
			names[string(l)] = v
		}
		if len(names) > 0 {
			out[id] = names
		}
	}
	return out, nil
}

// wrapMenuDup 唯一键冲突归一（v0.3：三个同级唯一键 → ErrSameLevelTaken → 30037）。
func wrapMenuDup(err error, op string) error {
	idx, dup := isDuplicate(err)
	if !dup {
		return fmt.Errorf("%s: %w", op, err)
	}
	switch idx {
	case "uk_menu_name", "uk_menu_path", "uk_menu_code":
		return menu.ErrSameLevelTaken
	}
	return fmt.Errorf("%s: %w", op, err)
}
