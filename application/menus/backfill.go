// 存量繁体中文译文补齐（docs《菜单繁体中文存量数据补齐建议.md》§3）：
// 以稳定 path 定位节点，只补缺失的 zh-TW，绝不覆盖人工译文；幂等——重复执行全跳过。
package menu

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	dmenu "dugu-hr-saas-admin/domain/menu"
	"dugu-hr-saas-admin/pkg/i18n"
)

// 存量繁体补齐条目：path 是唯一锚点（运营可能改名，不用 name 定位，docs §4）。
// 写入目标 = 统一翻译表 sys_i18n_text（res_type='menu'、field='name'；v0.6 起）。
type traditionalName struct {
	Path string
	TW   string
}

// traditionalNames 缺失清单（docs §2；与 zh-CN 默认名一一对应）。
// 后续新增目录/菜单时在此登记一条即可（新节点由运营接口直接配译文，本表仅兜底管理基线）。
var traditionalNames = []traditionalName{
	{"/organization", "組織管理"},
	{"/permission", "權限管控"},
	{"/permission/menus", "選單管理"},
	{"/tenant", "租戶管理"},
	{"/tenant/list", "租戶列表"},
	{"/tools", "基礎工具"},
	{"/tools/files", "檔案管理"},
	{"/operations", "系統維運"},
	{"/operations/site", "網站設定"},
	{"/operations/login", "登入設定"},
	{"/operations/dictionary", "字典工具"},
	{"/operations/audit", "稽核日誌"},
	{"/operations/login-logs", "登入日誌"},
	{"/operations/operation-logs", "操作日誌"},
}

// BackfillTraditionalNames 幂等补齐 zh-TW 译文。
// 返回 (补齐数, 跳过数(已有非空 zh-TW)、节点缺失数, 错误)。
// 只动统一翻译表（res_type='menu'/field='name'）的 zh-TW；不碰 zh-CN/en-US、主表 name、path/code/id_path（docs §4）。
func (s *Service) BackfillTraditionalNames(ctx context.Context) (filled, skipped, missing int, err error) {
	lang, ok := i18n.ParseLang("zh-TW")
	if !ok {
		return 0, 0, 0, errors.New("zh-TW 未注册，繁体补齐不可用（检查 pkg/i18n/locales）")
	}
	all, err := s.menus.ListAll(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	byPath := make(map[string]*dmenu.Menu, len(all))
	for i := range all {
		m := &all[i]
		if m.Type != dmenu.TypeDir && m.Type != dmenu.TypeMenu {
			continue // 动作节点译文已完整（docs §2），不处理
		}
		if _, exists := byPath[m.Path]; !exists {
			byPath[m.Path] = m // path 撞车（理论不可能）时取第一条，稳定幂等
		}
	}
	names, err := s.menus.NamesByIDs(ctx, idsOf(all))
	if err != nil {
		return 0, 0, 0, err
	}
	for _, item := range traditionalNames {
		m := byPath[item.Path]
		if m == nil {
			slog.Warn("繁体补齐：目录/菜单缺失，跳过", "path", item.Path)
			missing++
			continue
		}
		if strings.TrimSpace(names[m.ID][string(lang)]) != "" {
			skipped++ // 人工译文优先，绝不覆盖（docs §4）
			continue
		}
		if err := s.menus.UpsertNames(ctx, m.ID, map[string]string{string(lang): item.TW}); err != nil {
			return filled, skipped, missing, err
		}
		filled++
	}
	return filled, skipped, missing, nil
}
