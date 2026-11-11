// 资源 Resolver 实现（docs 基础设施-数据文案多语言(i18n)设计.md §7.3）：
// 由各资源在 infrastructure 落地，main 装配时经 RegisterI18nResolvers 注入 domain 注册表
// （domain 不反向 import infrastructure）。未注册的资源调译文管理接口 → 30038（验收 17）。
package dao

import (
	"context"
	"fmt"
	"log/slog"

	"dugu-hr-saas-admin/domain/authn"
	di18n "dugu-hr-saas-admin/domain/i18n"
	"dugu-hr-saas-admin/infrastructure/db"
	"dugu-hr-saas-admin/infrastructure/db/po"
)

// 编译期断言：满足领域端口。
var _ di18n.Resolver = (*menuI18nResolver)(nil)

// menuI18nResolver 菜单资源的存在性/快照/翻页（写作权限码 declares-and-reserves，docs §7.8）。
type menuI18nResolver struct{ d *db.DB }

func (m menuI18nResolver) Exists(ctx context.Context, id int64) (bool, error) {
	var cnt int64
	err := m.d.WithContext(ctx).Model(&po.SysMenu{}).
		Where("id = ? AND deleted_at = 0", id).Count(&cnt).Error
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func (m menuI18nResolver) Snapshot(ctx context.Context, ids []int64, field string) (map[int64]di18n.ResItem, error) {
	if field != "name" { // 菜单只翻 name（别处白名单也会拦）
		return map[int64]di18n.ResItem{}, nil
	}
	if len(ids) == 0 {
		return map[int64]di18n.ResItem{}, nil
	}
	var rows []po.SysMenu
	if err := m.d.WithContext(ctx).
		Select("id", "name").
		Where("deleted_at = 0 AND id IN ?", ids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]di18n.ResItem, len(rows))
	for _, p := range rows { // Key=标识（菜单就是 name 本身）；Value=默认语言列的值
		out[p.ID] = di18n.ResItem{ID: p.ID, Key: p.Name, Value: p.Name}
	}
	return out, nil
}

func (menuI18nResolver) PermWrite() string { return "menu:update" } // 声明并预留（§7.8）

// dictItemI18nResolver 字典项 Resolver——**随字典模块实现时注册**（§7.11 分期；
// 字典测试仅 DI 部分见 docs/系统管理-字典(sys_dict)设计.md 实施顺序）。
type dictItemI18nResolver struct{ d *db.DB }

func (dictItemI18nResolver) PermWrite() string { return "dict:update" } // 预留

// BackfillMenuI18nRows 存量菜单译文行回填（v1.14 用户裁定：所有菜单数据在
// 统一表初始化一份记录——与"菜单创建必插空行"语义对齐）。
// 幂等：只为"没有活译文行"的活菜单插空行（texts='{}'、mask/complete=0）；
// 已有行（含 zh-TW 等已录译文）一律不动。返回新建行数。
func BackfillMenuI18nRows(ctx context.Context, d *db.DB, ids authn.IDGenerator) (int64, error) {
	var menuIDs []int64
	if err := d.WithContext(ctx).Model(&po.SysMenu{}).
		Where("deleted_at = 0").
		Order("id ASC").Pluck("id", &menuIDs).Error; err != nil {
		return 0, fmt.Errorf("菜单行回填: 读活菜单: %w", err)
	}
	if len(menuIDs) == 0 {
		return 0, nil
	}
	have := map[int64]bool{}
	var rows []po.SysI18nText
	if err := d.WithContext(ctx).Model(&po.SysI18nText{}).
		Select("res_id").
		Where("res_type = ? AND field = ? AND deleted_at = 0", "menu", "name").
		Find(&rows).Error; err != nil {
		return 0, fmt.Errorf("菜单行回填: 读已有译文行: %w", err)
	}
	for _, r := range rows {
		have[r.ResID] = true
	}
	var n int64
	for _, mid := range menuIDs {
		if have[mid] {
			continue
		}
		row := po.SysI18nText{
			ID: ids.New(), ResType: "menu", ResID: mid, Field: "name", Texts: "{}",
		}
		if err := d.WithContext(ctx).Create(&row).Error; err != nil {
			return n, fmt.Errorf("菜单行回填: 插入 menu/%d: %w", mid, err)
		}
		n++
	}
	if n > 0 {
		slog.Info("菜单译文空行回填完成", "menus", len(menuIDs), "inserted", n)
	}
	return n, nil
}

// RegisterI18nResolvers 启动期把已落地资源的 Resolver 挂入 domain 注册表（幂等）。
// 目前：menu（已落地）。dict_item 待字典 DAO 实现后在此追加。
func RegisterI18nResolvers(d *db.DB) {
	di18n.Register(di18n.ResMenu, di18n.Spec{
		Label:    "菜单",
		Fields:   map[string]int{"name": 64},
		Resolver: menuI18nResolver{d: d},
	})
	// di18n.Register(di18n.ResDictItem, di18n.Spec{...}) // 字典模块落地后取消注释
}
