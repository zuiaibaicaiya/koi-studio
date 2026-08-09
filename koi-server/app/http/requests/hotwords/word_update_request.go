package hotwords

import (
	"github.com/goravel/framework/contracts/http"
)

// WordUpdateRequest 更新热词请求，仅更新传入的字段
type WordUpdateRequest struct {
	// Word 热词内容
	Word string `form:"word" json:"word" maxLength:"100" example:"人工智能"`
	// Weight 热词权重，为空时不更新
	Weight *int `form:"weight" json:"weight" example:"10"`
}

func (r *WordUpdateRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *WordUpdateRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"word": "trim",
	}
}

func (r *WordUpdateRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"word":   "max_len:100",
		"weight": "int|min:0|max:10000",
	}
}

func (r *WordUpdateRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"word.max_len": ":attribute长度最多%d位",
		"weight.int":   ":attribute必须为整数",
		"weight.min":   ":attribute不能小于%d",
		"weight.max":   ":attribute不能大于%d",
	}
}

func (r *WordUpdateRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"word":   "热词内容",
		"weight": "权重",
	}
}
