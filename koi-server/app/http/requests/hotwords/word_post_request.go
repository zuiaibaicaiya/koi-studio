package hotwords

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// WordPostRequest 创建热词请求
type WordPostRequest struct {
	// Word 热词内容
	Word string `form:"word" json:"word" maxLength:"100" example:"人工智能"`
	// Weight 热词权重
	Weight int `form:"weight" json:"weight" example:"10"`
}

func (r *WordPostRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *WordPostRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"word": "trim",
	}
}

func (r *WordPostRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"word":   "required|max_len:100",
		"weight": "int|min:0|max:10000",
	}
}

func (r *WordPostRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"word.required": ":attribute不可以为空",
		"word.max_len":  ":attribute长度最多%d位",
		"weight.int":    ":attribute必须为整数",
		"weight.min":    ":attribute不能小于%d",
		"weight.max":    ":attribute不能大于%d",
	}
}

func (r *WordPostRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"word":   "热词内容",
		"weight": "权重",
	}
}

func (r *WordPostRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	if _, exist := data.Get("weight"); !exist {
		data.Set("weight", 0)
	}

	return nil
}
