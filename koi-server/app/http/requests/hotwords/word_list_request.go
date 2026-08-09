package hotwords

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// WordListRequest 热词列表查询请求
type WordListRequest struct {
	// Page 页码
	Page int `form:"page" json:"page" example:"1"`
	// PageSize 每页数量
	PageSize int `form:"pageSize" json:"pageSize" example:"16"`
	// Keyword 搜索关键词
	Keyword string `form:"keyword" json:"keyword" example:"人工智能"`
}

func (r *WordListRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *WordListRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"keyword": "trim",
	}
}

func (r *WordListRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"page":     "min:1",
		"pageSize": "min:1",
	}
}

func (r *WordListRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"page.min":     "页码至少为1",
		"pageSize.min": "每页数量至少为1",
	}
}

func (r *WordListRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"page":     "页码",
		"pageSize": "每页数量",
	}
}

func (r *WordListRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	if _, exist := data.Get("page"); !exist {
		data.Set("page", 1)
	}

	if _, exist := data.Get("pageSize"); !exist {
		data.Set("pageSize", 16)
	}

	return nil
}
