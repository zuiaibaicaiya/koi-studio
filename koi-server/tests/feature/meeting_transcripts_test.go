package feature

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"koi-server/app/facades"
	"koi-server/app/models"
	"koi-server/tests"
)

// MeetingTranscriptsIntegrationTestSuite 前后端联调测试：
// 直接落库会议与转写记录（不加载 ASR 大模型），通过真实 HTTP 链路请求
// GET /api/meeting/{id}/transcripts，并断言返回结构与前端 MeetingTranscriptDTO 完全对齐。
type MeetingTranscriptsIntegrationTestSuite struct {
	suite.Suite
	tests.TestCase
	token     string
	meetingID uint
}

func TestMeetingTranscriptsIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(MeetingTranscriptsIntegrationTestSuite))
}

func (s *MeetingTranscriptsIntegrationTestSuite) SetupSuite() {
	// 1. 注册/登录测试用户，拿到 JWT
	username := fmt.Sprintf("autotest_transcripts_%d", time.Now().UnixNano())
	resp, err := s.Http(s.T()).Post("/api/user/register",
		strings.NewReader(fmt.Sprintf(`{"username":"%s","password":"test123","nickname":"转写联调测试"}`, username)))
	s.Require().NoError(err)
	resp.AssertOk()

	var regResult struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	s.Require().NoError(resp.Bind(&regResult))
	if regResult.Code != 0 {
		// 用户已存在则改用登录
		resp, err = s.Http(s.T()).Post("/api/user/login",
			strings.NewReader(fmt.Sprintf(`{"username":"%s","password":"test123"}`, username)))
		s.Require().NoError(err)
		resp.AssertOk()
		s.Require().NoError(resp.Bind(&regResult))
	}
	s.Require().Equal(0, regResult.Code)
	s.Require().NotEmpty(regResult.Data.Token)
	s.token = regResult.Data.Token

	// 2. 创建会议
	createResp, err := s.Http(s.T()).WithToken(s.token, "Bearer").Post("/api/meeting",
		strings.NewReader(`{"name":"转写联调测试会议","participants":"张三、李四","start_time":"2026-08-16 09:00:00","end_time":"2026-08-16 10:00:00"}`))
	s.Require().NoError(err)
	createResp.AssertOk()

	var createResult struct {
		Code int `json:"code"`
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	s.Require().NoError(createResp.Bind(&createResult))
	s.Require().Equal(0, createResult.Code)
	s.Require().NotZero(createResult.Data.ID)
	s.meetingID = createResult.Data.ID

	// 3. 直接落库转写记录（is_final=true，按 start_ms 升序）
	seed := []models.MeetingTranscript{
		{MeetingID: s.meetingID, SpeakerID: nil, SpeakerName: "未知说话人", Text: "会议现在开始。", StartMs: 1000, EndMs: 2500, IsFinal: true},
		{MeetingID: s.meetingID, SpeakerID: ptr(uint(1)), SpeakerName: "张三", Text: "下面由我汇报上周进度。", StartMs: 3000, EndMs: 6200, IsFinal: true},
		{MeetingID: s.meetingID, SpeakerID: ptr(uint(2)), SpeakerName: "李四", Text: "我补充一下测试结论。", StartMs: 6500, EndMs: 9800, IsFinal: true},
	}
	for i := range seed {
		s.Require().NoError(facades.Orm().Query().Create(&seed[i]))
	}
}

func (s *MeetingTranscriptsIntegrationTestSuite) TearDownSuite() {
	if s.meetingID != 0 {
		_, _ = facades.Orm().Query().Model(&models.MeetingTranscript{}).
			Where("meeting_id = ?", s.meetingID).Delete()
		_, _ = facades.Orm().Query().Model(&models.Meeting{}).
			Where("id = ?", s.meetingID).Delete()
	}
}

// TestGetMeetingTranscriptsHTTP 真实 HTTP 请求转写分页接口，校验前后端契约。
func (s *MeetingTranscriptsIntegrationTestSuite) TestGetMeetingTranscriptsHTTP() {
	resp, err := s.Http(s.T()).WithToken(s.token, "Bearer").
		Get(fmt.Sprintf("/api/meeting/%d/transcripts?page=1&pageSize=50", s.meetingID))
	s.Require().NoError(err)
	resp.AssertOk()

	root, err := resp.Json()
	s.Require().NoError(err)
	s.Equal(float64(0), root["code"], "业务码应为 0")

	data, ok := root["data"].(map[string]any)
	s.Require().True(ok, "data 应为对象")
	s.Equal(float64(3), data["total"], "total 应为 3")
	s.Equal(float64(1), data["page"])
	s.Equal(float64(50), data["pageSize"])

	items, ok := data["items"].([]any)
	s.Require().True(ok, "items 应为数组")
	s.Require().Len(items, 3)

	// 校验返回结构与前端 MeetingTranscriptDTO 字段一一对应（snake_case）
	expectedFields := []string{
		"id", "meeting_id", "speaker_id", "speaker_name",
		"text", "start_ms", "end_ms", "word_timestamps", "is_final",
	}
	for i, it := range items {
		item, ok := it.(map[string]any)
		s.Require().True(ok, "items[%d] 应为对象", i)
		for _, f := range expectedFields {
			_, present := item[f]
			s.Truef(present, "返回缺少字段 %q（前端 DTO 需要）", f)
		}
		// 校验按 start_ms 升序
		s.Equal(float64(s.meetingID), item["meeting_id"])
		s.Equal(true, item["is_final"])
	}

	// 校验排序：首条应为 start_ms 最小的记录
	first := items[0].(map[string]any)
	s.Equal("会议现在开始。", first["text"])
	s.Equal(float64(1000), first["start_ms"])

	// 打印一条样本，便于人工核对前后端字段
	s.T().Logf("联调样本: %s", mustMarshal(first))
}

func ptr[T any](v T) *T { return &v }

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
