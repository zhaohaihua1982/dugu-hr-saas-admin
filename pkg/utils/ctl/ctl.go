// Package ctl 统一响应封装（gin-mall 风格）。
package ctl

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/pkg/code"
	"dugu-hr-saas-admin/pkg/utils/log"
)

// Response 基础序列化器：HTTP 状态恒 200，业务语义在 status。
type Response struct {
	Status int         `json:"status"`
	Msg    string      `json:"msg"`
	Data   interface{} `json:"data"`
}

// RespSuccess 成功返回。传 data 为 nil 时填 "ok"。
func RespSuccess(ctx *gin.Context, data interface{}) {
	if data == nil {
		data = "ok"
	}
	ctx.JSON(http.StatusOK, Response{
		Status: code.SUCCESS,
		Msg:    code.GetMsg(code.SUCCESS),
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
		Msg:    code.GetMsg(status),
		Data:   data,
	})
}
