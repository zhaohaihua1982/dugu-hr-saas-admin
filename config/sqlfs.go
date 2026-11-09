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

// SQLFiles 迁移执行顺序：主表在前，日志表在后。
var SQLFiles = []string{"sql/sys_user.sql", "sql/sys_login_log.sql", "sql/sys_menu.sql", "sql/sys_menu_i18n.sql"}

// ReadSQL 读取内嵌迁移脚本内容。
func ReadSQL(name string) (string, error) {
	b, err := sqlFS.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(b)), nil
}
