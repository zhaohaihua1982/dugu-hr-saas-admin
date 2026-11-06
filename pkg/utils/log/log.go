// Package log 统一日志出口：业务日志 + 安全审计事件。
package log

import (
	"fmt"
	"log/slog"
	"os"
)

// auditLogger 审计专用：单独一组属性（action/detail），便于接入企业监控。
var (
	logger *slog.Logger
	audit  *slog.Logger
)

func init() {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger = slog.New(h)
	audit = slog.New(h).With(slog.String("kind", "AUDIT"))
}

// Infof 业务信息日志。
func Infof(format string, args ...interface{}) {
	logger.Info(fmt.Sprintf(format, args...))
}

// Warnf 业务告警（如登录日志异步写失败——需要监控告警的场景）。
func Warnf(format string, args ...interface{}) {
	logger.Warn(fmt.Sprintf(format, args...))
}

// Errorf 业务错误日志。
func Errorf(format string, args ...interface{}) {
	logger.Error(fmt.Sprintf(format, args...))
}

// Audit 安全审计事件（docs §8.3：对超管账号的一切操作强制审计）。
// action 建议用 "域.动作"，如 sysuser.reset_password / sysuser.delete。
// detail 为 key→value 结构，逐项展开为结构化属性。
func Audit(action string, detail map[string]interface{}) {
	args := make([]interface{}, 0, (len(detail)+1)*2)
	atLeastOne := false
	for k, v := range detail {
		args = append(args, k, v)
		atLeastOne = true
	}
	if !atLeastOne {
		audit.Info(action)
		return
	}
	audit.Info(action, args...)
}
