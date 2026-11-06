package service

import "errors"

// ErrIncorrectRange 日志查询时间区间格式不合法。
var ErrIncorrectRange = errors.New("时间格式不合法（YYYY-MM-DD）")
