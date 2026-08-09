package users

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// UserListRequest 用户列表查询请求
type UserListRequest struct {
	// Page 页码
	Page int `form:"page" json:"page" example:"1"`
	// PageSize 每页数量
	PageSize int `form:"pageSize" json:"pageSize" example:"16"`
	// Keyword 搜索关键词
	Keyword string `form:"keyword" json:"keyword" example:"张三"`
	// Status 状态筛选
	Status string `form:"status" json:"status" example:"active"`
}

func (r *UserListRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *UserListRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"keyword": "trim",
	}
}

func (r *UserListRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"page":     "min:1",
		"pageSize": "min:1",
		"status":   "in:active,inactive",
	}
}

func (r *UserListRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"page.min":     "页码至少为1",
		"pageSize.min": "每页数量至少为1",
		"status.in":    ":attribute值不正确",
	}
}

func (r *UserListRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"page":     "页码",
		"pageSize": "每页数量",
		"status":   "状态",
	}
}

func (r *UserListRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	// 设置默认值（仅当参数未提供时）
	if _, exist := data.Get("page"); !exist {
		data.Set("page", 1)
	}

	if _, exist := data.Get("pageSize"); !exist {
		data.Set("pageSize", 16)
	}

	return nil
}
