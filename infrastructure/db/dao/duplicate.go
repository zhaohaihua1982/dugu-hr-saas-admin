// dao 包内小工具：唯一键冲突归类（gorm 错误链解包到驱动错误）。
package dao

import (
	"errors"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// isDuplicate 判断错误是否唯一键冲突（1062），并返回命中的索引名。
// "" 表示冲突但未能识别索引。
func isDuplicate(err error) (string, bool) {
	var mysqlErr *mysqldriver.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		return "", false
	}
	switch {
	case strings.Contains(mysqlErr.Message, "uk_username"):
		return "uk_username", true
	case strings.Contains(mysqlErr.Message, "uk_mobile"):
		return "uk_mobile", true
	case strings.Contains(mysqlErr.Message, "uk_action"):
		return "uk_action", true
	}
	return "", true
}
