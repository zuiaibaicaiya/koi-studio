package hotwords

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// LibraryPostRequest 创建热词库请求
type LibraryPostRequest struct {
	// Name 热词库名称
	Name string `form:"name" json:"name" maxLength:"100" example:"通用行业热词"`
	// Description 热词库描述
	Description string `form:"description" json:"description" maxLength:"255" example:"用于语音识别的通用行业热词"`
	// Status 状态
	Status string `form:"status" json:"status" example:"active"`
}

func (r *LibraryPostRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *LibraryPostRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"name":        "trim",
		"description": "trim",
	}
}

func (r *LibraryPostRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"name":        "required|max_len:100",
		"description": "max_len:255",
		"status":      "in:active,inactive",
	}
}

func (r *LibraryPostRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"name.required":       ":attribute不可以为空",
		"name.max_len":        ":attribute长度最多%d位",
		"description.max_len": ":attribute长度最多%d位",
		"status.in":           ":attribute值不正确",
	}
}

func (r *LibraryPostRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"name":        "热词库名称",
		"description": "热词库描述",
		"status":      "状态",
	}
}

func (r *LibraryPostRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	if _, exist := data.Get("status"); !exist {
		data.Set("status", "active")
	}

	return nil
}
