package meetings

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/validation"
)

// MeetingListRequest 实时会议列表查询请求
type MeetingListRequest struct {
	// Page 页码
	Page int `form:"page" json:"page" example:"1"`
	// PageSize 每页数量
	PageSize int `form:"pageSize" json:"pageSize" example:"16"`
	// Keyword 搜索关键词（会议名称/参会人员）
	Keyword string `form:"keyword" json:"keyword" example:"周会"`
	// Status 状态筛选：created/ongoing/finished
	Status string `form:"status" json:"status" example:"created"`
	// Mode 模式筛选：live-实时会议，audio-音频转写
	Mode string `form:"mode" json:"mode" example:"live"`
	// StartTime 时间段起始（筛选该时间段内的会议，格式 2006-01-02 15:04:05）
	StartTime string `form:"start_time" json:"start_time" example:"2026-08-01 00:00:00"`
	// EndTime 时间段结束
	EndTime string `form:"end_time" json:"end_time" example:"2026-08-31 23:59:59"`
}

func (r *MeetingListRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *MeetingListRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"keyword":    "trim",
		"status":     "trim",
		"mode":       "trim",
		"start_time": "trim",
		"end_time":   "trim",
	}
}

func (r *MeetingListRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"page":     "min:1",
		"pageSize": "min:1",
		"status":   "in:created,ongoing,finished",
		"mode":     "in:live,audio",
	}
}

func (r *MeetingListRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"page.min":     "页码至少为1",
		"pageSize.min": "每页数量至少为1",
		"status.in":    ":attribute值不正确",
		"mode.in":      ":attribute值不正确",
	}
}

func (r *MeetingListRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"page":       "页码",
		"pageSize":   "每页数量",
		"status":     "状态",
		"mode":       "模式",
		"start_time": "开始时间",
		"end_time":   "结束时间",
	}
}

func (r *MeetingListRequest) PrepareForValidation(ctx http.Context, data validation.Data) error {
	if _, exist := data.Get("page"); !exist {
		data.Set("page", 1)
	}

	if _, exist := data.Get("pageSize"); !exist {
		data.Set("pageSize", 16)
	}

	return nil
}
