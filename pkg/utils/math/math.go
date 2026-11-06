// Package math 提供验证码类随机串生成（安全敏感：必须 crypto/rand）。
package math

import (
	"crypto/rand"
	"math/big"
)

// RandDigits 生成 n 位纯数字验证码（去 0 开头难以输入的顾虑不存在：首位也可为 0）。
func RandDigits(n int) string {
	digits := "0123456789"
	out := make([]byte, n)
	for i := range out {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		out[i] = digits[idx.Int64()]
	}
	return string(out)
}

// RandHex 生成 n 字节随机 hex（用于图形验证码 id 等）。
func RandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	const hexDig = "0123456789abcdef"
	for i, c := range b {
		out := hexDig[int(c)%16]
		b[i] = out
	}
	return string(b)
}
