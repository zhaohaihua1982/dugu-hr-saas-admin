// Package i18n 按业务错误码（pkg/code 数字码）提供多语言文案。
//
// 设计（延续 go-demo 的可插拔思路）：
//   - 新增一门语言：pkg/i18n/locales/ 放同名 json（如 ja-JP.json），
//     编译期内嵌自动注册，不改任何 Go 代码；
//   - 默认语言优先级：yaml i18n.defaultLang（bootstrap 启动时应用）
//     > 环境变量 SAAS_LANG > 内置回退 zh-CN；
//   - 请求级语言：?lang= 或 Accept-Language（含 q 权重），接口层经
//     i18n.FromGin 解析，未识别时回退默认语言；
//   - 文案键为错误码字符串（如 "30010"），缺失时回退同语言包 "500"，
//     语言包整体缺失时回退任意已注册语言包。
package i18n

import (
	"embed"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Lang 语言标签，即 locales/ 下的文件名（不含扩展名）。
type Lang string

// LangZH / LangEN 内置语言。
const (
	LangZH Lang = "zh-CN"
	LangEN Lang = "en-US"
)

// fallbackLang 任何配置都未命中时的回退语言。
const fallbackLang = LangZH

//go:embed locales/*.json
var localeFS embed.FS

var (
	once    sync.Once
	bundles map[Lang]map[string]string
	// explicit 默认语言（yaml 配置在启动时应用），nil 表示未显式设置。
	explicit atomic.Pointer[Lang]
)

// Key 通用文案键（非错误码的界面字符串，如发送结果提示）。
const (
	KeySmsSent = "sms.sent"
)

// load 编译期内嵌数据：扫描 locales/ 注册全部语言包。
func load() {
	once.Do(func() {
		entries, err := localeFS.ReadDir("locales")
		if err != nil {
			return
		}
		bundles = make(map[Lang]map[string]string, len(entries))
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".json") {
				continue
			}
			data, err := localeFS.ReadFile("locales/" + name)
			if err != nil {
				continue
			}
			var m map[string]string
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			bundles[Lang(strings.TrimSuffix(name, ".json"))] = m
		}
	})
}

// normalize 归一化写法：大小写不敏感、下划线视为连字符。
func normalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), "_", "-"))
}

// ParseLang 严格解析：必须命中已注册语言（en_us / EN-US 均可）；裸 "en" 不算。
func ParseLang(s string) (Lang, bool) {
	norm := normalize(s)
	for lang := range Registered() {
		if norm == normalize(string(lang)) {
			return lang, true
		}
	}
	return "", false
}

// Registered 已注册语言集合（键=Lang，值=条目数）。
func Registered() map[Lang]int {
	load()
	out := make(map[Lang]int, len(bundles))
	for l, b := range bundles {
		out[l] = len(b)
	}
	return out
}

// Languages 已注册语言排序切片。
func Languages() []Lang {
	load()
	langs := make([]Lang, 0, len(bundles))
	for l := range bundles {
		langs = append(langs, l)
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs
}

// Supported 语言包是否注册。
func Supported(l Lang) bool { _, ok := ParseLang(string(l)); return ok }

// SetDefaultLang 显式设定默认语言（bootstrap 应用 yaml i18n.defaultLang）。
func SetDefaultLang(l Lang) {
	load()
	if Supported(l) {
		explicit.Store(&l)
	}
}

// Reset 清除显式默认语言（测试隔离）。
func Reset() { explicit.Store(nil) }

// Default 默认语言：显式设置 > SAAS_LANG > fallback。
func Default() Lang {
	if p := explicit.Load(); p != nil {
		return *p
	}
	if l, ok := ParseLang(os.Getenv("SAAS_LANG")); ok {
		return l
	}
	return fallbackLang
}

// FromAccept 解析 Accept-Language：取第一个 guest 已注册的标签（q 权重排序，
// 同权重取先出现者）。裸主语言（en/zh）按已注册集合唯一命中时映射。
func FromAccept(header string) Lang {
	if strings.TrimSpace(header) == "" {
		return Default()
	}
	type cand struct {
		lang Lang
		q    float64
	}
	var list []cand
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, q := part, 1.0
		if i := strings.IndexByte(part, ';'); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			if j := strings.Index(part[i:], "="); j > 0 && i+1+j < len(part) {
				if f, err := strconv.ParseFloat(strings.TrimSpace(part[i+j+1:]), 64); err == nil {
					q = f
				}
			}
		}
		list = append(list, cand{lang: Lang(tag), q: q})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].q > list[j].q })

	// 已注册完整标签集合（含主语言映射）
	langs := Languages()
	for _, c := range list {
		if l, ok := ParseLang(string(c.lang)); ok {
			return l
		}
	}
	for _, c := range list { // 裸主语言（en → en-US）唯一命中时映射
		p := normalize(string(c.lang))
		if !strings.Contains(p, "-") {
			var m Lang
			n := 0
			for _, l := range langs {
				if strings.HasPrefix(normalize(string(l)), p+"-") {
					m, n = l, n+1
				}
			}
			if n == 1 {
				return m
			}
		}
	}
	return Default()
}

// Msg 按 lang 取业务码对应文案（缺失回退该语言包 "500"，语言包缺失走默认链）。
func Msg(lang Lang, bizCode int) string {
	return Get(lang, strconv.Itoa(bizCode))
}

// Get 按 lang 取任意键文案（含 KeySmsSent 等通用键）。
func Get(lang Lang, key string) string {
	load()
	if b, ok := bundles[lang]; ok {
		return fromBundle(b, key)
	}
	if d := Default(); d != lang {
		if b, ok := bundles[d]; ok {
			return fromBundle(b, key)
		}
	}
	for _, b := range bundles { // 最终兜底：任意已注册语言包
		return fromBundle(b, key)
	}
	return ""
}

// fromBundle 单语言包取键，缺失退 "500"。
func fromBundle(b map[string]string, key string) string {
	if m, ok := b[key]; ok && m != "" {
		return m
	}
	return b["500"]
}
