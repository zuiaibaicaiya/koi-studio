package api

import (
	"time"

	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"

	"koi-server/app/facades"
	"koi-server/app/models"
)

// BaseController 提供统一的响应格式与通用辅助方法
type BaseController struct {
}

// GetFirstError 提取验证错误中的第一条错误信息
func (base *BaseController) GetFirstError(errors validation.Errors) string {
	if errors == nil {
		return ""
	}

	errorsMap := errors.All()
	if len(errorsMap) == 0 {
		return ""
	}

	msg := ""
	for key := range errorsMap {
		msg = errors.One(key)
		break
	}

	return msg
}

// ApiSuccess 返回成功响应
func (base *BaseController) ApiSuccess(ctx http.Context, data any) http.Response {
	return ctx.Response().Json(http.StatusOK, http.Json{
		"code": 0,
		"msg":  "success",
		"data": data,
	})
}

// ApiErrorMsg 返回默认错误码的错误响应
func (base *BaseController) ApiErrorMsg(ctx http.Context, msg string) http.Response {
	return base.ApiError(ctx, 1, msg)
}

// ApiError 返回指定错误码的错误响应
func (base *BaseController) ApiError(ctx http.Context, code int, msg string) http.Response {
	return ctx.Response().Json(http.StatusOK, http.Json{
		"code":       code,
		"msg":        msg,
		"timestamp":  time.Now().Unix(),
		"request_id": ctx.Value("request_id"),
	})
}

// ApiPaginate 返回分页响应
func (base *BaseController) ApiPaginate(ctx http.Context, data any, total int64, page int, pageSize int) http.Response {
	totalPage := int64(0)
	if pageSize > 0 {
		totalPage = (total + int64(pageSize) - 1) / int64(pageSize)
	}

	return ctx.Response().Json(http.StatusOK, http.Json{
		"code": 0,
		"msg":  "success",
		"data": map[string]any{
			"items":     data,
			"total":     total,
			"page":      page,
			"pageSize":  pageSize,
			"totalPage": totalPage,
		},
	})
}

// GetCurrentUser 获取当前登录用户
func (base *BaseController) GetCurrentUser(ctx http.Context) (models.User, error) {
	var user models.User
	err := facades.Auth(ctx).User(&user)

	return user, err
}
