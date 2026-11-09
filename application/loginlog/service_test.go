// 登录日志用例单测：时间范围解析契约（秒级、拒绝毫秒/坏格式）与默认当天窗口。
package loginlog

import (
	"testing"
	"time"
)

func TestParseRangeValid(t *testing.T) {
	now := time.Now()

	// 纯日期 end → 含当日全天
	from, to, err := parseRange("2026-11-09", "2026-11-09")
	if err != nil {
		t.Fatalf("纯日期应合法: %v", err)
	}
	if from.Hour() != 0 || from.Minute() != 0 || from.Second() != 0 {
		t.Fatalf("start 应为 00:00:00: %v", from)
	}
	if !(to.Hour() == 23 && to.Minute() == 59 && to.Second() == 59) {
		t.Fatalf("end 应为 23:59:59（含当日）: %v", to)
	}

	// 秒级起止
	from2, to2, err := parseRange(
		now.Format(timeFmtDateTime),
		now.Format(timeFmtDateTime))
	if err != nil {
		t.Fatalf("秒级格式应合法: %v", err)
	}
	if from2 != to2 {
		t.Fatalf("同值起止应相等: %v vs %v", from2, to2)
	}
}

func TestParseRangeRejectMilliseconds(t *testing.T) {
	// Go time.Parse 会隐式接受小数秒；这里必须显式拒绝（docs：不收毫秒）
	for _, bad := range []string{
		"2026-11-09 00:00:00.123",
		"2026-11-09T00:00:00.123",
		"2026-11-09.123",
	} {
		if _, _, err := parseRange(bad, ""); err == nil {
			t.Fatalf("毫秒/小数点输入应被拒绝: %q", bad)
		}
	}
}

func TestParseRangeRejectBadFormats(t *testing.T) {
	for _, bad := range []string{"abc", "2026/11/09", "2026-11-09 12:00", "2026-13-01"} {
		if _, _, err := parseRange(bad, ""); err == nil {
			t.Fatalf("非法格式应被拒绝: %q", bad)
		}
	}
}

func TestParseRangeEmptyMeansNoFilter(t *testing.T) {
	from, to, err := parseRange("", "")
	if err != nil {
		t.Fatalf("空串不限时: %v", err)
	}
	if !from.IsZero() || !to.IsZero() {
		t.Fatal("空串应表示不过滤")
	}
}
