package cache

import (
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// itoa 小工具：int64 → string（避免多处 strconv 噪音）。
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

// isNilErr redis.Nil 归一化（键不存在不算错误）。
func isNilErr(err error) bool {
	return errors.Is(err, redis.Nil)
}

// dayWindow “一天”窗口统一为 24h 固定窗口（精确到自然日会引入时区/边界复杂度，够用）。
const dayWindow = 24 * time.Hour
