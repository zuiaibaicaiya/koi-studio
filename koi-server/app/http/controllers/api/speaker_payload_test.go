package api

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	contractsaudio "koi-server/app/contracts/audio"
	"koi-server/app/services"
)

// SpeakerPayloadTestSuite 覆盖 register-speaker 事件（框选转写文字注册说话人）
// 的参数解析与失败提示。
type SpeakerPayloadTestSuite struct {
	suite.Suite
}

func TestSpeakerPayloadTestSuite(t *testing.T) {
	suite.Run(t, new(SpeakerPayloadTestSuite))
}

func (s *SpeakerPayloadTestSuite) TestParseSpeakerRegistrationSnakeCase() {
	requestID, req, err := parseSpeakerRegistration([]any{map[string]any{
		"meeting_id": float64(12),
		"start_ms":   float64(1500),
		"end_ms":     float64(6200),
		"name":       " 王小明 ",
		"description": "后端负责人",
		"text":       "我们今天先对齐一下接口协议",
	}})

	s.Require().NoError(err)
	s.Empty(requestID)
	s.Equal(uint(12), req.MeetingID)
	s.Equal(int64(1500), req.StartMs)
	s.Equal(int64(6200), req.EndMs)
	s.Equal("王小明", req.Name, "名称应去除首尾空白")
	s.Equal("后端负责人", req.Description)
	s.Equal("我们今天先对齐一下接口协议", req.Text)
}

func (s *SpeakerPayloadTestSuite) TestParseSpeakerRegistrationCamelCase() {
	// 前端 socket.io 客户端可能直接发送驼峰字段，协议层需同时兼容。
	requestID, req, err := parseSpeakerRegistration([]any{map[string]any{
		"requestId": "req-1",
		"meetingId": float64(3),
		"startMs":   float64(0),
		"endMs":     float64(4000),
		"speakerName": "李四",
	}})

	s.Require().NoError(err)
	s.Equal("req-1", requestID)
	s.Equal(uint(3), req.MeetingID)
	s.Equal(int64(0), req.StartMs)
	s.Equal(int64(4000), req.EndMs)
	s.Equal("李四", req.Name)
}

func (s *SpeakerPayloadTestSuite) TestParseSpeakerRegistrationRejectsBadInput() {
	cases := []struct {
		name          string
		args          []any
		expectedError string
	}{
		{"缺少参数", nil, "缺少请求参数"},
		{"参数类型错误", []any{"not-an-object"}, "请求参数格式不正确"},
		{
			"缺少名称",
			[]any{map[string]any{"start_ms": float64(0), "end_ms": float64(3000)}},
			"请填写说话人名称",
		},
		{
			"名称为空白",
			[]any{map[string]any{"start_ms": float64(0), "end_ms": float64(3000), "name": "  "}},
			"请填写说话人名称",
		},
		{
			"未框选文本",
			[]any{map[string]any{"name": "王小明"}},
			"请先框选要归属到该说话人的转写文字",
		},
		{
			"区间倒序",
			[]any{map[string]any{"name": "王小明", "start_ms": float64(5000), "end_ms": float64(1000)}},
			"请先框选要归属到该说话人的转写文字",
		},
		{
			"片段过短",
			[]any{map[string]any{"name": "王小明", "start_ms": float64(1000), "end_ms": float64(1500)}},
			"请至少框选 1 秒的转写文字",
		},
	}

	for _, item := range cases {
		s.Run(item.name, func() {
			_, _, err := parseSpeakerRegistration(item.args)

			s.Require().Error(err)
			s.Contains(err.Error(), item.expectedError)
		})
	}
}

func (s *SpeakerPayloadTestSuite) TestParseSpeakerRegistrationRequiresMinSegment() {
	// 恰好 1 秒是允许的下限。
	_, req, err := parseSpeakerRegistration([]any{map[string]any{
		"name":     "王小明",
		"start_ms": float64(2000),
		"end_ms":   float64(3000),
	}})

	s.Require().NoError(err)
	s.Equal(int64(1000), req.EndMs-req.StartMs)
}

func (s *SpeakerPayloadTestSuite) TestSpeakerRegistrationErrorMessage() {
	cases := []struct {
		name      string
		err       error
		expected  string
	}{
		{
			name:     "缺少名称",
			err:      contractsaudio.ErrSpeakerNameRequired,
			expected: contractsaudio.ErrSpeakerNameRequired.Error(),
		},
		{
			name:     "名称过长",
			err:      contractsaudio.ErrSpeakerNameTooLong,
			expected: contractsaudio.ErrSpeakerNameTooLong.Error(),
		},
		{
			name:     "未绑定会议",
			err:      contractsaudio.ErrMeetingNotBound,
			expected: contractsaudio.ErrMeetingNotBound.Error(),
		},
		{
			name:     "会议不一致",
			err:      contractsaudio.ErrMeetingMismatch,
			expected: contractsaudio.ErrMeetingMismatch.Error(),
		},
		{
			name:     "有效语音不足",
			err:      fmt.Errorf("%w: 有效语音时长仅 0.60 秒", services.ErrValidSpeechTooShort),
			expected: "有效语音时长不足: 有效语音时长仅 0.60 秒",
		},
		{
			name:     "声纹模型不可用",
			err:      contractsaudio.ErrSpeakerRegistrationUnavailable,
			expected: "声纹模型不可用，暂时无法注册说话人",
		},
		{
			name:     "内部错误不暴露细节",
			err:      errors.New("audio: session not found"),
			expected: "注册说话人失败，请确认会议正在转写后重试",
		},
	}

	for _, item := range cases {
		s.Run(item.name, func() {
			s.Equal(item.expected, speakerRegistrationErrorMessage(item.err))
		})
	}
}

func (s *SpeakerPayloadTestSuite) TestSpeakerRegistrationErrorMessageForNil() {
	s.Empty(speakerRegistrationErrorMessage(nil))
}

func (s *SpeakerPayloadTestSuite) TestMinSpeakerSegmentBoundary() {
	// 保证协议层与实现层对「最小时长」的理解一致：低于该值不进入声纹提取。
	s.Equal(1000, minSpeakerSegmentMs)
	s.True(strings.HasSuffix(contractsaudio.ErrSpeakerNameTooLong.Error(), "32 个字符"))
}
