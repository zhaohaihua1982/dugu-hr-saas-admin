// Package ctl 统一响应封装。msg 按请求语言（?lang= / Accept-Language）生成，
// 未协商到时回退配置默认语言（pkg/i18n）。
package ctl

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/i18n"
	"dugu-hr-saas-admin/pkg/utils/log"
)

// Response 基础序列化器：HTTP 状态恒 200，业务语义在 status。
type Response struct {
	Status int         `json:"status"`
	Msg    string      `json:"msg"`
	Data   interface{} `json:"data"`
}

// lang 请求语言优先级：?lang= > Accept-Language > 配置默认（pkg/i18n.Default）。
func lang(ctx *gin.Context) i18n.Lang {
	if v := ctx.Query("lang"); v != "" {
		if l, ok := i18n.ParseLang(v); ok {
			return l
		}
	}
	return i18n.FromAccept(ctx.GetHeader("Accept-Language"))
}

// MsgOf 按请求语言取业务码文案（供 middleware 等不直接持 i18n 的场景复用）。
func MsgOf(ctx *gin.Context, bizCode int) string {
	return i18n.Msg(lang(ctx), bizCode)
}

// RespSuccess 成功返回。data 为 nil 时填 "ok"。
func RespSuccess(ctx *gin.Context, data interface{}) {
	if data == nil {
		data = "ok"
	}
	ctx.JSON(http.StatusOK, Response{
		Status: code.SUCCESS,
		Msg:    i18n.Msg(lang(ctx), code.SUCCESS),
		Data:   data,
	})
}

// RespError 错误返回。err 仅记录日志不回传，对外信息只有业务码与文案；
// data 可为附加字段（如 remaining_seconds / captcha_required）。
func RespError(ctx *gin.Context, err error, status int, data interface{}) {
	if err != nil {
		log.Errorf("业务错误: status=%d err=%v", status, err)
	}
	ctx.JSON(http.StatusOK, Response{
		Status: status,
		Msg:    i18n.Msg(lang(ctx), status),
		Data:   data,
	})
}
