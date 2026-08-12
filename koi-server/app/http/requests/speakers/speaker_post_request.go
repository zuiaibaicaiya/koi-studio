package speakers

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// SpeakerPostRequest 创建说话人请求
type SpeakerPostRequest struct {
	// Name 说话人名称，全局唯一
	Name string `form:"name" json:"name" maxLength:"100" example:"张三"`
	// Description 说话人描述
	Description string `form:"description" json:"description" maxLength:"255" example:"技术部产品经理"`
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
		"description": "max_len:255",
	}
}

func (r *SpeakerPostRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"name.required":       ":attribute不可以为空",
		"name.max_len":        ":attribute长度最多%d位",
		"description.max_len": ":attribute长度最多%d位",
	}
}

func (r *SpeakerPostRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"name":        "说话人名称",
		"description": "说话人描述",
	}
}

func (r *SpeakerPostRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	return nil
}
