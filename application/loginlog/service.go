// Package loginlog —— 登录日志查询用例（docs §9 GET /login-logs）。
package loginlog

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dugu-hr-saas-admin/domain/loginlog"
	"dugu-hr-saas-admin/domain/sysuser"
)

// ---------- 用例输入/输出对象 ----------

// LoginLogResp 登录日志条目（id/user_id 用字符串：雪花 ID 超出 JS 安全整数，见 UserView 注释）。
type LoginLogResp struct {
	ID             string              `json:"id"`
	UserID         string              `json:"user_id"`
	IdentifierMask string              `json:"identifier_mask"`
	Type           string              `json:"login_type"`
	Via            string              `json:"ident_via"`
	Success        bool                `json:"success"`
	Reason         loginlog.FailReason `json:"fail_reason"`
	IP             string              `json:"ip"`
	UA             string              `json:"ua"`
	At             string              `json:"created_at"`
}

// ListLoginLogsReq 登录日志查询（StartAt/EndAt 为 YYYY-MM-DD，空串不限）。
type ListLoginLogsReq struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	UserID   int64  `form:"user_id"`
	IP       string `form:"ip"`
	// Identifier 登录标识查询：调用方传**原始输入**（账号或手机号均可，
	// 也兼容直接传已脱敏的 mask），应用层按与入库同规则脱敏后精确匹配。
	Identifier string `form:"identifier" binding:"omitempty,max=128"`
	Success    *bool  `form:"success"`
	StartAt    string `form:"start_at"` // YYYY-MM-DD（含当日）
	EndAt      string `form:"end_at"`
}

// ListLoginLogsResp 登录日志响应。
type ListLoginLogsResp struct {
	Items    []LoginLogResp `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// ErrBadRange 时间区间格式不合法（映射 code.InvalidParams）。
var ErrBadRange = fmt.Errorf("loginlog: bad time range")

// ---------- 用例 ----------

// Service 日志查询用例。
type Service struct {
	logs loginlog.Repository
}

// NewService 构造。
func NewService(logs loginlog.Repository) *Service { return &Service{logs} }

// List 日志查询（标识已按 docs §3.2 v1.2 规则落库，不经手明文）。
// 默认时间窗口：**start/end 都不传时，只查“今天”**（当天 00:00:00 ~ 23:59:59.999）。
func (s *Service) List(ctx context.Context, req *ListLoginLogsReq) (*ListLoginLogsResp, error) {
	startAt, endAt := req.StartAt, req.EndAt
	if startAt == "" && endAt == "" { // 默认窗口：只查当天
		today := time.Now().Format(timeFmtDate)
		startAt, endAt = today, today
	}
	from, to, err := parseRange(startAt, endAt)
	if err != nil {
		return nil, err
	}
	res, err := s.logs.Query(ctx, loginlog.Query{
		Page:     req.Page,
		PageSize: req.PageSize,
		UserID:   req.UserID,
		IP:       req.IP,
		// 与入库同一脱敏规则（docs §3.2“存脱敏标识”），精确命中 idx_log_identifier
		IdentifierMask: maskQueryIdentifier(req.Identifier),
		Success:        req.Success,
		TimeFrom:       from,
		TimeTo:         to,
	})
	if err != nil {
		return nil, err
	}
	items := make([]LoginLogResp, 0, len(res.Items))
	for i := range res.Items {
		l := &res.Items[i]
		items = append(items, LoginLogResp{
			ID:             strconv.FormatInt(l.ID, 10),
			UserID:         strconv.FormatInt(l.UserID, 10),
			IdentifierMask: l.IdentifierMask,
			Type:           string(l.Type),
			Via:            string(l.Via),
			Success:        l.Success,
			Reason:         l.Reason,
			IP:             l.IP,
			UA:             l.UA,
			At:             l.At.Format(time.DateTime),
		})
	}
	return &ListLoginLogsResp{Items: items, Total: res.Total, Page: res.Page, PageSize: res.PageSize}, nil
}

const maskWildcard = "***"

// maskQueryIdentifier 查询标识 → 入库值（与落库同规则，v1.2）：
//   - 形似手机号 → 138****8000；
//   - 其余（账号）→ **原样**（与登录日志新规一致）；历史 mask（如 ad***in）说明见 docs §3.2 注。
//
// 返回空串表示不过滤。
func maskQueryIdentifier(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, maskWildcard) {
		return raw // 直接给了历史 mask（兼容老数据查询）
	}
	return sysuser.LogIdentifier(raw)
}

// 时间入参格式（docs §13.3 v1.2）：YYYY-MM-DD HH:mm:ss 或纯日期（兼容）。
// **秒级精度，不收毫秒**：Go 的 time.Parse 会隐式接受小数秒（即使布局未写），
// 若不显式拒绝，毫秒参数将悄悄改变过滤口径——与“响应不携带语言惯性”同一审计口径，
// 入参侧同样拒绝 Go 的隐式行为（见 parseOne 的点号守卫）。
const (
	timeFmtDateTime = "2006-01-02 15:04:05"
	timeFmtDate     = "2006-01-02"
)

// parseRange 解析时间范围（空串不限）。
//   - start 为纯日期 → 当日 00:00:00 起；
//   - end   为纯日期 → **含末日全天**（+24h-1ms 截止）；带时分秒则精确到秒。
func parseRange(startAt, endAt string) (time.Time, time.Time, error) {
	var from, to time.Time
	// parseOne 只接受秒级精度的两种格式；任何带小数点的值一律拒绝
	//（防 time.Parse 隐式解析毫秒/微秒尾巴，docs 响应合同“不收毫秒”）。
	parseOne := func(v string) (time.Time, bool) {
		if strings.Contains(v, ".") {
			return time.Time{}, false
		}
		for _, f := range []string{timeFmtDateTime, timeFmtDate} {
			if t, err := time.ParseInLocation(f, v, time.Local); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	if startAt != "" {
		t, ok := parseOne(startAt)
		if !ok {
			return from, to, ErrBadRange
		}
		from = t
	}
	if endAt != "" {
		t, ok := parseOne(endAt)
		if !ok {
			return from, to, ErrBadRange
		}
		if len(endAt) == 10 { // 纯日期 → 含末日全天（23:59:59.999 截止属于内部实现，对入参仍不收毫秒）
			to = t.Add(24*time.Hour - time.Millisecond)
		} else {
			to = t
		}
	}
	return from, to, nil
}
