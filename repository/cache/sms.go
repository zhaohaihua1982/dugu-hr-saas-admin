package cache

import (
	"errors"

	"github.com/redis/go-redis/v9"

	"dugu-hr-saas-admin/consts"
)

// ---------- 图形验证码（一次性，docs §5 前置 / §8.7） ----------

// SaveCaptcha 存图形验证码答案（TTL 5 分钟）。
func SaveCaptcha(id, answer string) error {
	return Client().Set(ctxBG, consts.KeyCaptcha+id, answer, consts.CaptchaExpire).Err()
}

// VerifyCaptcha 校验并立即删除（用后即销毁，防重放）。
// 属“必须命中”型检查：Redis 键不存在即失败。
func VerifyCaptcha(id, answer string) (bool, error) {
	if id == "" || answer == "" {
		return false, nil
	}
	key := consts.KeyCaptcha + id
	expect, err := Client().GetDel(ctxBG, key).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil // 过期或已用
	}
	if err != nil {
		return false, err
	}
	return expect == answer, nil
}

// ---------- 短信验证码（docs §5：5 分钟、一次性、试错 5 次作废、用途隔离） ----------

// SaveSmsCode 存登录用途验证码：HASH{code, tries}，TTL 5 分钟。
func SaveSmsCode(purpose, mobileHash, code string) error {
	key := smsKey(purpose, mobileHash)
	return Client().HSet(ctxBG, key, map[string]interface{}{
		"code":  code,
		"tries": 0,
	}).Err()
}

type smsVerifyResult int

const (
	smsVerifyOK        smsVerifyResult = iota
	smsVerifyBadResult                 // 码不对（计数 +1）
	smsVerifyRetried                   // 试错已达上限，验证码作废
	smsVerifyNotFound                  // 过期/已用/不存在
)

// VerifySmsCode 校验验证码（用途含在 key 内，登录码不能用来改密码）。
func VerifySmsCode(purpose, mobileHash, code string) (smsVerifyResult, error) {
	key := smsKey(purpose, mobileHash)

	val, err := Client().HGet(ctxBG, key, "code").Result()
	if errors.Is(err, redis.Nil) {
		return smsVerifyNotFound, nil
	}
	if err != nil {
		return smsVerifyNotFound, err
	}
	if val == code {
		// 用后即删（防重放）
		if err := Client().Del(ctxBG, key).Err(); err != nil {
			return smsVerifyNotFound, err
		}
		return smsVerifyOK, nil
	}

	tries, err := Client().HIncrBy(ctxBG, key, "tries", 1).Result()
	if err != nil {
		return smsVerifyBadResult, err
	}
	if int(tries) >= consts.SmsMaxVerifyTries {
		// 错满 5 次验证码作废，需重新发送
		if err := Client().Del(ctxBG, key).Err(); err != nil {
			return smsVerifyRetried, err
		}
		return smsVerifyRetried, nil
	}
	return smsVerifyBadResult, nil
}

// ---------- 短信发送配额（docs §5：双端限流） ----------

// SmsMinuteAllowed 单手机 1 条/分钟。
func SmsMinuteAllowed(purpose, mobileHash string) (bool, error) {
	n, err := incrWindow(smsMinuteKey(purpose, mobileHash), consts.SmsMinuteLimit)
	return n <= int64(consts.SmsMinuteLimitMax), err
}

// SmsDailyAllowed 单手机 10 条/天。
func SmsDailyAllowed(purpose, mobileHash string) (bool, error) {
	n, err := incrWindow(smsDayKey(purpose, mobileHash), dayWindow)
	return n <= int64(consts.SmsDailyLimitMax), err
}

// SmsIPDailyAllowed 单 IP 100 条/天。
func SmsIPDailyAllowed(ip string) (bool, error) {
	n, err := incrWindow(smsIPDayKey(ip), dayWindow)
	return n <= int64(consts.SmsIPDailyLimitMax), err
}

// smsKey sms:code:{用途}:{手机hash}。
func smsKey(purpose, mobileHash string) string {
	return "sms:code:" + purpose + ":" + mobileHash
}
func smsMinuteKey(purpose, mobileHash string) string {
	return "sms:min:" + purpose + ":" + mobileHash
}
func smsDayKey(purpose, mobileHash string) string {
	return "sms:day:" + purpose + ":" + mobileHash
}
func smsIPDayKey(ip string) string {
	return "sms:ipday:" + ip
}
