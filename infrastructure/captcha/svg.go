// Package captcha 图形验证码：纯 SVG 文本 + 随机位移/旋转，零外部图片依赖。
// 一次性存储（Redis）在 infrastructure/cache.CaptchaService；本包只负责“画”。
package captcha

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"

	"dugu-hr-saas-admin/pkg/utils/math"
)

// captchaLength 图形码位数（docs §5）。
const captchaLength = 5

var palette = []string{"#4262ff", "#0958d9", "#1d39c4", "#062f46", "#365314"}

// Generate 生成一题：返回 id、答案明文、data:image/svg+xml;base64 图。
// answer 长度 captchaLength，数字型（5 位）。
func Generate() (id, answer, imageDataURL string, err error) {
	answer = math.RandDigits(captchaLength)
	id = math.RandHex(16)

	svg := renderSVG(answer)
	imageDataURL = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	return id, answer, imageDataURL, nil
}

// renderSVG 每个字符独立旋转/位移/配色，肉眼可读但干扰简单 OCR。
func renderSVG(value string) string {
	w, h, n := 160, 48, len(value)
	var b strings.Builder

	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="`)
	b.WriteString(itoa(w))
	b.WriteString(`" height="`)
	b.WriteString(itoa(h))
	b.WriteString(`" viewBox="0 0 160 48">`)
	b.WriteString(`<rect width="160" height="48" rx="8" fill="#f5f5f5"/>`)

	// 干扰线
	for i := 0; i < 4; i++ {
		b.WriteString(`<path d="M`)
		b.WriteString(itoa(randWithin(0, w/3)))
		b.WriteString(` `)
		b.WriteString(itoa(randWithin(4, h)))
		b.WriteString(` L`)
		b.WriteString(itoa(randWithin(w*2/3, w)))
		b.WriteString(` `)
		b.WriteString(itoa(randWithin(4, h)))
		b.WriteString(`" stroke="`)
		b.WriteString(palette[i%len(palette)])
		b.WriteString(`" stroke-width="1" fill="none" opacity="0.35"/>`)
	}

	step := (w - 30) / n
	for i, ch := range value {
		x := 22 + i*step
		y := 32 + randWithin(-4, 4)
		rot := randWithin(-25, 25)
		b.WriteString(`<text x="`)
		b.WriteString(itoa(x))
		b.WriteString(`" y="`)
		b.WriteString(itoa(y))
		b.WriteString(`" font-size="30" font-family="Helvetica,Arial,sans-serif" font-weight="700" fill="`)
		b.WriteString(palette[i%len(palette)])
		b.WriteString(`" transform="rotate(`)
		b.WriteString(itoa(rot))
		b.WriteString(` `)
		b.WriteString(itoa(x))
		b.WriteString(` `)
		b.WriteString(itoa(y))
		b.WriteString(`)">`)
		b.WriteRune(ch)
		b.WriteString(`</text>`)
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func randWithin(lo, hi int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(hi-lo)))
	return lo + int(n.Int64())
}

func itoa(v int) string {
	return fmt.Sprintf("%d", v)
}
