package conf

import (
	"bytes"
	"embed"
)

// sqlFS 编译期内嵌 config/sql 下的 DDL 迁移脚本。
// 启动时由 dao 包按序幂等执行（CREATE TABLE IF NOT EXISTS）。
//
//go:embed sql/*.sql
var sqlFS embed.FS

// SQLFiles 迁移执行顺序：主表在前，日志表在后；统一译文表先于所有引用它的业务表。
var SQLFiles = []string{
	"sql/sys_user.sql",
	"sql/sys_login_log.sql",
	"sql/sys_i18n_text.sql",
	"sql/sys_dict_type.sql", // 字典类型表（迁移器一次执行一条语句，两表分两文件，docs 字典设计 §1.2）
	"sql/sys_dict_item.sql", // 字典项表（label 译文存统一表，故排在 sys_i18n_text.sql 之后）
	"sql/sys_menu.sql",
	"sql/sys_login_config.sql",      // 登录配置表（docs 登录配置设计 §3.1，单语句文件）
	"sql/sys_login_config_init.sql", // 登录配置 seed（单条 INSERT IGNORE 两行，§3.2）
	"sql/sys_password_history.sql",  // 历史密码表（docs 登录配置设计 §10 P1.5）
}

// ReadSQL 读取内嵌迁移脚本内容。
func ReadSQL(name string) (string, error) {
	b, err := sqlFS.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(b)), nil
}
