// 资源 Resolver 实现（docs 基础设施-数据文案多语言(i18n)设计.md §7.3）：
// 由各资源在 infrastructure 落地，main 装配时经 RegisterI18nResolvers 注入 domain 注册表
// （domain 不反向 import infrastructure）。未注册的资源调译文管理接口 → 30038（验收 17）。
package dao

import (
	"context"

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
