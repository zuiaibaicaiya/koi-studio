package speakers

import (
	"github.com/goravel/framework/contracts/http"
)

// SpeakerUpdateRequest 更新说话人请求，仅更新传入的字段
type SpeakerUpdateRequest struct {
	// Name 说话人名称，全局唯一
	Name string `form:"name" json:"name" maxLength:"100" example:"张三"`
	// Description 说话人描述
	Description string `form:"description" json:"description" maxLength:"255" example:"技术部产品经理"`
}

func (r *SpeakerUpdateRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *SpeakerUpdateRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"name":        "trim",
		"description": "trim",
	}
}

func (r *SpeakerUpdateRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"name":        "max_len:100",
		"description": "max_len:255",
	}
}

func (r *SpeakerUpdateRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"name.max_len":        ":attribute长度最多%d位",
		"description.max_len": ":attribute长度最多%d位",
	}
}

func (r *SpeakerUpdateRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"name":        "说话人名称",
		"description": "说话人描述",
	}
}
