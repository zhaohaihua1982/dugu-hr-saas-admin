// 鉴权白名单：未登录即可访问的 URL 集合。
// 规则（只看 path，与 method/query 无关）：
//   - "/a/b"        精确匹配；
//   - "/a/b/*"     前缀通配（须带路径边界：匹配 /a/b/ 下所有子路径，不吞 /a/b 本身）。
package middleware

import "strings"

// Whitelist 白名单匹配器。
type Whitelist struct {
	exact    map[string]bool
	prefixes []string
}

// NewWhitelist 构造（paths 可混用 exact 与通配，如 "/healthz"、"/api/v1/admin/auth/*"）。
func NewWhitelist(paths ...string) *Whitelist {
	w := &Whitelist{exact: make(map[string]bool, len(paths))}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			w.prefixes = append(w.prefixes, strings.TrimSuffix(p, "*")) // 保留结尾 "/"（路径边界）
		} else {
			w.exact[p] = true
		}
	}
	return w
}

// Match 是否命中白名单（只看 path，不看 method/query）。
func (w *Whitelist) Match(path string) bool {
	if w == nil {
		return false
	}
	if w.exact[path] {
		return true
	}
	for _, p := range w.prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
