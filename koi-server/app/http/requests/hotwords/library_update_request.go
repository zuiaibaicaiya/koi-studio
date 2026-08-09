package hotwords

import (
	"github.com/goravel/framework/contracts/http"
)

// LibraryUpdateRequest 更新热词库请求，仅更新传入的字段
type LibraryUpdateRequest struct {
	// Name 热词库名称
	Name string `form:"name" json:"name" maxLength:"100" example:"通用行业热词"`
	// Description 热词库描述
	Description string `form:"description" json:"description" maxLength:"255" example:"用于语音识别的通用行业热词"`
	// Status 状态
	Status string `form:"status" json:"status" example:"active"`
}

func (r *LibraryUpdateRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *LibraryUpdateRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"name":        "trim",
		"description": "trim",
	}
}

func (r *LibraryUpdateRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"name":        "max_len:100",
		"description": "max_len:255",
		"status":      "in:active,inactive",
	}
}

func (r *LibraryUpdateRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"name.max_len":        ":attribute长度最多%d位",
		"description.max_len": ":attribute长度最多%d位",
		"status.in":           ":attribute值不正确",
	}
}

func (r *LibraryUpdateRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"name":        "热词库名称",
		"description": "热词库描述",
		"status":      "状态",
	}
}
