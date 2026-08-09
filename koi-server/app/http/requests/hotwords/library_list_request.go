package hotwords

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// LibraryListRequest 热词库列表查询请求
type LibraryListRequest struct {
	// Page 页码
	Page int `form:"page" json:"page" example:"1"`
	// PageSize 每页数量
	PageSize int `form:"pageSize" json:"pageSize" example:"16"`
	// Keyword 搜索关键词
	Keyword string `form:"keyword" json:"keyword" example:"行业"`
	// Status 状态筛选
	Status string `form:"status" json:"status" example:"active"`
}

func (r *LibraryListRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *LibraryListRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"keyword": "trim",
	}
}

func (r *LibraryListRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"page":     "min:1",
		"pageSize": "min:1",
		"status":   "in:active,inactive",
	}
}

func (r *LibraryListRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"page.min":     "页码至少为1",
		"pageSize.min": "每页数量至少为1",
		"status.in":    ":attribute值不正确",
	}
}

func (r *LibraryListRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"page":     "页码",
		"pageSize": "每页数量",
		"status":   "状态",
	}
}

func (r *LibraryListRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	if _, exist := data.Get("page"); !exist {
		data.Set("page", 1)
	}

	if _, exist := data.Get("pageSize"); !exist {
		data.Set("pageSize", 16)
	}

	return nil
}
