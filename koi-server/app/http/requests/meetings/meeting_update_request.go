package meetings

import (
	"github.com/goravel/framework/contracts/http"
)

// MeetingUpdateRequest 更新实时会议请求（字段均可选）
type MeetingUpdateRequest struct {
	// Name 会议名称（纯文本）
	Name string `form:"name" json:"name" maxLength:"100" example:"产品周会"`
	// Participants 参会人员（纯文本）
	Participants string `form:"participants" json:"participants" maxLength:"500" example:"张三、李四"`
	// SpeakerIds 说话人ID列表，逗号分隔
	SpeakerIds string `form:"speaker_ids" json:"speaker_ids" maxLength:"500" example:"1,2,3"`
	// HotWordLibraryIds 关联的热词库ID列表，逗号分隔（空表示不修改）
	HotWordLibraryIds string `form:"hot_word_library_ids" json:"hot_word_library_ids" maxLength:"500" example:"1,2,3"`
	// StartTime 开始时间
	StartTime string `form:"start_time" json:"start_time" example:"2026-08-10 09:00:00"`
	// EndTime 结束时间
	EndTime string `form:"end_time" json:"end_time" example:"2026-08-10 10:00:00"`
	// Status 会议状态：created-已创建，ongoing-进行中，finished-已结束
	Status string `form:"status" json:"status" example:"created"`
}

func (r *MeetingUpdateRequest) Authorize(ctx http.Context) error {
	return nil
}

func (r *MeetingUpdateRequest) Filters(ctx http.Context) map[string]any {
	return map[string]any{
		"name":         "trim",
		"participants": "trim",
		"speaker_ids":  "trim",
		"status":       "trim",
	}
}

func (r *MeetingUpdateRequest) Rules(ctx http.Context) map[string]any {
	return map[string]any{
		"name":               "max_len:100",
		"participants":       "max_len:500",
		"speaker_ids":        "max_len:500",
		"hot_word_library_ids": "max_len:500",
		"status":             "in:created,ongoing,finished",
	}
}

func (r *MeetingUpdateRequest) Messages(ctx http.Context) map[string]string {
	return map[string]string{
		"name.max_len":  ":attribute长度最多%d位",
		"status.in":     ":attribute值不正确",
	}
}

func (r *MeetingUpdateRequest) Attributes(ctx http.Context) map[string]string {
	return map[string]string{
		"name":               "会议名称",
		"participants":       "参会人员",
		"speaker_ids":        "说话人",
		"hot_word_library_ids": "热词库",
		"start_time":         "开始时间",
		"end_time":           "结束时间",
		"status":             "状态",
	}
}
