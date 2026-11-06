package v1

import (
	"github.com/gin-gonic/gin"

	"dugu-hr-saas-admin/pkg/utils/ctl"
	"dugu-hr-saas-admin/service"
	"dugu-hr-saas-admin/types"
)

// ListLoginLogsHandler GET /admin/login-logs。
func ListLoginLogsHandler() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req types.ListLoginLogsReq
		if err := ctx.ShouldBindQuery(&req); err != nil {
			ErrorResponse(ctx, err)
			return
		}
		resp, err := service.GetLoginLogSrv().List(ctx, &req)
		if err != nil {
			ErrorResponse(ctx, err)
			return
		}
		ctl.RespSuccess(ctx, resp)
	}
}
