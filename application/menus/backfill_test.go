// 存量繁体补齐单测（docs《菜单繁体中文存量数据补齐建议.md》§5）：
// 只补缺失、不覆盖人工译文、幂等（第二次全跳过）、缺失 path 会计数。
// 用例分支：命中+已有人工译文（跳过）/ 命中+缺失（补齐）/ 未命中（missing） / 动作节点（不处理）。
package menu

import (
	"context"
	"testing"

	dmenu "dugu-hr-saas-admin/domain/menu"
)

// fakeRepo 内嵌接口桩，只实现本用例需要的方法。
type fakeRepo struct {
	dmenu.Repository
	names map[int64]map[string]string
}

func (f *fakeRepo) ListAll(context.Context) ([]dmenu.Menu, error) {
	return []dmenu.Menu{
		{ID: 101, Name: "菜单管理", Path: "/permission/menus", Type: dmenu.TypeMenu},   // 命中表 + 已有人工译文
		{ID: 104, Name: "租户列表", Path: "/tenant/list", Type: dmenu.TypeMenu},        // 命中表 + 缺失
		{ID: 105, Name: "用户管理", Path: "/organization/users", Type: dmenu.TypeMenu}, // 不在表内（人工值节点）
		{ID: 103, Name: "删除", Path: "/whatever", Type: dmenu.TypeAction},           // 动作不处理
	}, nil
}

func (f *fakeRepo) NamesByIDs(_ context.Context, ids []int64) (map[int64]map[string]string, error) {
	out := map[int64]map[string]string{}
	for _, id := range ids {
		if n, ok := f.names[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func (f *fakeRepo) UpsertNames(_ context.Context, menuID int64, patch map[string]string) error {
	if f.names[menuID] == nil {
		f.names[menuID] = map[string]string{}
	}
	for k, v := range patch {
		f.names[menuID][k] = v
	}
	return nil
}

func TestBackfillTraditionalNames(t *testing.T) {
	repo := &fakeRepo{
		names: map[int64]map[string]string{
			101: {"zh-TW": "人工译文"}, // 人工值优先：不允许覆盖（docs §4）
		},
	}
	s := &Service{menus: repo}
	ctx := context.Background()

	// 第一次：104 补齐；101 保人工值；动作不建行；其余表项节点缺失
	filled, skipped, missing, err := s.BackfillTraditionalNames(ctx)
	if err != nil {
		t.Fatalf("首次补齐失败: %v", err)
	}
	if filled != 1 || skipped != 1 || missing != 12 {
		t.Fatalf("计数不符: filled=%d skipped=%d missing=%d", filled, skipped, missing)
	}
	if got := repo.names[104]["zh-TW"]; got != "租戶列表" {
		t.Fatalf("104 应补为 租戶列表, got %q", got)
	}
	if got := repo.names[101]["zh-TW"]; got != "人工译文" {
		t.Fatalf("人工译文被覆盖: %q", got)
	}
	if _, has := repo.names[103]; has {
		if repo.names[103]["zh-TW"] != "" {
			t.Fatal("动作节点不应建译文行")
		}
	}

	// 第二次：幂等——命中节点全为跳过，无新增
	filled2, skipped2, missing2, err := s.BackfillTraditionalNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if filled2 != 0 || skipped2 != 2 || missing2 != 12 {
		t.Fatalf("第二次计数不符: filled=%d skipped=%d missing=%d", filled2, skipped2, missing2)
	}
}
