package speakers

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// SpeakerListRequest 说话人列表查询请求
type SpeakerListRequest struct {
	// Page 页码
	Page int `form:"page" json:"page" example:"1"`
	// PageSize 每页数量
	PageSize int `form:"pageSize" json:"pageSize" example:"16"`
	// Keyword 关键词，匹配名称或描述
	Keyword string `form:"keyword" json:"keyword" example:"张三"`
	// Gender 性别筛选
	Gender string `form:"gender" json:"gender" example:"male"`
	// Status 状态筛选
	Status string `form:"status" json:"status" example:"active"`
}

func (r *SpeakerListRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *SpeakerListRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"keyword": "trim",
	}
}

func (r *SpeakerListRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"page":     "int|min:1",
		"pageSize": "int|min:1|max:100",
		"keyword":  "max_len:100",
		"gender":   "in:male,female,unknown",
		"status":   "in:active,inactive",
	}
}

func (r *SpeakerListRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"page.min":        ":attribute最小为%d",
		"pageSize.min":    ":attribute最小为%d",
		"pageSize.max":    ":attribute最大为%d",
		"keyword.max_len": ":attribute长度最多%d位",
		"gender.in":       ":attribute值不正确",
		"status.in":       ":attribute值不正确",
	}
}

func (r *SpeakerListRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"page":     "页码",
		"pageSize": "每页数量",
		"keyword":  "关键词",
		"gender":   "性别",
		"status":   "状态",
	}
}

func (r *SpeakerListRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	return nil
}
