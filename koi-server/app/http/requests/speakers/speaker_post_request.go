package speakers

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// SpeakerPostRequest 创建说话人请求
type SpeakerPostRequest struct {
	// Name 说话人名称，全局唯一
	Name string `form:"name" json:"name" maxLength:"100" example:"张三"`
	// Gender 性别：unknown-未知，male-男，female-女
	Gender string `form:"gender" json:"gender" example:"male"`
	// Description 说话人描述
	Description string `form:"description" json:"description" maxLength:"255" example:"技术部产品经理"`
	// Status 状态：active-启用，inactive-禁用
	Status string `form:"status" json:"status" example:"active"`
}

func (r *SpeakerPostRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *SpeakerPostRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"name":        "trim",
		"description": "trim",
	}
}

func (r *SpeakerPostRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"name":        "required|max_len:100",
		"gender":      "in:male,female,unknown",
		"description": "max_len:255",
		"status":      "in:active,inactive",
	}
}

func (r *SpeakerPostRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"name.required":       ":attribute不可以为空",
		"name.max_len":        ":attribute长度最多%d位",
		"gender.in":           ":attribute值不正确",
		"description.max_len": ":attribute长度最多%d位",
		"status.in":           ":attribute值不正确",
	}
}

func (r *SpeakerPostRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"name":        "说话人名称",
		"gender":      "性别",
		"description": "说话人描述",
		"status":      "状态",
	}
}

func (r *SpeakerPostRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	if _, exist := data.Get("gender"); !exist {
		data.Set("gender", "unknown")
	}

	if _, exist := data.Get("status"); !exist {
		data.Set("status", "active")
	}

	return nil
}
