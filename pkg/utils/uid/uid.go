// Package uid 雪花 ID 生成（sys_user/sys_login_log 主键，应用侧生成）。
package uid

import (
	"fmt"

	"github.com/bwmarrin/snowflake"
)

var node *snowflake.Node

func init() {
	n, err := snowflake.NewNode(1)
	if err != nil {
		panic(fmt.Errorf("初始化雪花发生器: %w", err))
	}
	node = n
}

// New 生成全局唯一 ID。
func New() int64 {
	return node.Generate().Int64()
}
