package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRemoveSpeakerID 覆盖「删除说话人时从会议关联列表剔除该 ID」的纯函数行为。
func TestRemoveSpeakerID(t *testing.T) {
	cases := []struct {
		name     string
		ids      string
		id       uint
		expected string
		removed  bool
	}{
		{"移除首位", "1,2,3", 1, "2,3", true},
		{"移除中间", "1,2,3", 2, "1,3", true},
		{"移除末位", "1,2,3", 3, "1,2", true},
		{"仅剩一个", "2", 2, "", true},
		{"不存在则原样返回", "1,2", 3, "1,2", false},
		{"容忍空白字符", " 1 , 2 , 3 ", 2, " 1 , 3 ", true},
		{"空列表", "", 1, "", false},
		{"零值ID不参与移除", "1,2", 0, "1,2", false},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			got, removed := removeSpeakerID(item.ids, item.id)
			assert.Equal(t, item.expected, got)
			assert.Equal(t, item.removed, removed)
		})
	}
}
