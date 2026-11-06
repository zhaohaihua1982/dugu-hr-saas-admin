// main —— dugu-hr-saas-admin 启动入口（gin-mall 标准结构）。
// 路径：docs/SaaS管理后台-管理账号(sys_user)设计.md P0~P1.5。
package main

import (
	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/repository/cache"
	"dugu-hr-saas-admin/repository/db/dao"
	"dugu-hr-saas-admin/routes"
)

func main() {
	loading() // 加载配置、迁移建表、seed 超管、预热依赖
	r := routes.NewRouter()
	_ = r.Run(conf.Config.System.HttpPort)
}

// loading 配置 → MySQL（含迁移与 seed 超管）→ Redis。
func loading() {
	conf.InitConfig()
	dao.InitMySQL()
	cache.InitCache()
}
