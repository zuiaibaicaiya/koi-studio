package feature

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"koi-server/app/facades"
	"koi-server/app/models"
	"koi-server/tests"
)

// MeetingTestSuite 实时会议接口自动化测试
type MeetingTestSuite struct {
	suite.Suite
	tests.TestCase
	token string
}

func TestMeetingTestSuite(t *testing.T) {
	suite.Run(t, new(MeetingTestSuite))
}

// SetupSuite 注册测试用户并获取 JWT 令牌，整个套件复用同一令牌。
func (s *MeetingTestSuite) SetupSuite() {
	resp, err := s.Http(s.T()).Post("/api/user/register",
		strings.NewReader(`{"username":"autotest_meeting","password":"test123","nickname":"自动化测试"}`))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	resp.AssertOk()

	// 如果注册失败（可能用户已存在），尝试登录
	var regResult struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := resp.Bind(&regResult); err != nil {
		s.Require().NoError(err)
	}

	if regResult.Code != 0 {
		// 用户已存在，走登录
		resp, err = s.Http(s.T()).Post("/api/user/login",
			strings.NewReader(`{"username":"autotest_meeting","password":"test123"}`))
		s.Require().NoError(err)
		s.Require().NotNil(resp)
		resp.AssertOk()
		s.Require().NoError(resp.Bind(&regResult))
	}

	s.Require().Equal(0, regResult.Code, "register/login should succeed")
	s.Require().NotEmpty(regResult.Data.Token, "token should not be empty")
	s.token = regResult.Data.Token
}

func (s *MeetingTestSuite) SetupTest()  {}
func (s *MeetingTestSuite) TearDownTest() {}

// TestCreateMeetingSuccess 验证成功创建会议——最小必填参数。
func (s *MeetingTestSuite) TestCreateMeetingSuccess() {
	body := `{"name":"测试会议","participants":"张三、李四"}`
	resp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting", strings.NewReader(body))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	resp.AssertOk()

	var result struct {
		Code int            `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	s.Require().NoError(resp.Bind(&result))

	s.Equal(0, result.Code, "response code should be 0")
	s.Equal("success", result.Msg)
	s.Equal("测试会议", result.Data["name"])
	s.Equal("张三、李四", result.Data["participants"])
	s.Equal("created", result.Data["status"])
	s.NotZero(result.Data["id"], "meeting ID should not be zero")

	// 未提供时间时应自动填充默认值
	s.NotEmpty(result.Data["start_time"])
	s.NotEmpty(result.Data["end_time"])
}

// TestCreateMeetingWithFullFields 验证携带全部可选参数创建会议。
func (s *MeetingTestSuite) TestCreateMeetingWithFullFields() {
	body := `{
		"name": "完整参数会议",
		"participants": "张三、李四、王五",
		"speaker_ids": "1,2,3",
		"hot_word_library_ids": "1,2",
		"start_time": "2026-08-11 09:00:00",
		"end_time": "2026-08-11 10:00:00"
	}`
	resp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting", strings.NewReader(body))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	resp.AssertOk()

	var result struct {
		Code int            `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	s.Require().NoError(resp.Bind(&result))

	s.Equal(0, result.Code)
	s.Equal("完整参数会议", result.Data["name"])
	s.Equal("1,2,3", result.Data["speaker_ids"])
	s.Equal("1,2", result.Data["hot_word_library_ids"])
	s.Equal("created", result.Data["status"])
	// 创建人应被记录
	s.NotZero(result.Data["created_by"])
}

// TestCreateMeetingWithoutAuth 验证未认证请求被拒绝。
func (s *MeetingTestSuite) TestCreateMeetingWithoutAuth() {
	body := `{"name":"无认证会议"}`
	resp, err := s.Http(s.T()).Post("/api/meeting", strings.NewReader(body))
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// JWT 中间件可能返回 401 而非 200
	// 根据项目约定，检查是否返回了错误
	content, err := resp.Content()
	s.Require().NoError(err)

	var result map[string]any
	s.Require().NoError(json.Unmarshal([]byte(content), &result))

	// 无 token 时应返回非 0 的 code 或非 200 的 HTTP 状态
	s.True(
		result["code"] != float64(0) || !resp.IsSuccessful(),
		"request without token should be rejected",
	)
}

// TestCreateMeetingEmptyName 验证名称为空时参数校验。
func (s *MeetingTestSuite) TestCreateMeetingEmptyName() {
	body := `{"name":"","participants":"张三"}`
	resp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting", strings.NewReader(body))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	resp.AssertOk()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	s.Require().NoError(resp.Bind(&result))

	s.NotEqual(0, result.Code, "empty name should trigger validation error")
	s.Contains(result.Msg, "会议名称", "error message should mention meeting name")
}

// TestCreateMeetingInvalidTime 验证结束时间早于开始时间时被拦截。
func (s *MeetingTestSuite) TestCreateMeetingInvalidTime() {
	body := `{
		"name": "时间非法会议",
		"start_time": "2026-08-11 10:00:00",
		"end_time": "2026-08-11 09:00:00"
	}`
	resp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting", strings.NewReader(body))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	resp.AssertOk()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	s.Require().NoError(resp.Bind(&result))

	s.NotEqual(0, result.Code, "end_time before start_time should be rejected")
	s.Contains(result.Msg, "结束时间", "error message should mention end time")
}

// TestCreateMeetingNameTooLong 验证名称超长被校验拦截。
func (s *MeetingTestSuite) TestCreateMeetingNameTooLong() {
	longName := strings.Repeat("长", 101) // max_len:100
	body, err := json.Marshal(map[string]string{
		"name":         longName,
		"participants": "test",
	})
	s.Require().NoError(err)

	resp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting", strings.NewReader(string(body)))
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	resp.AssertOk()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	s.Require().NoError(resp.Bind(&result))

	s.NotEqual(0, result.Code, "overlong name should trigger validation error")
}

// TestMeetingListReturnsAccessibleAudioURL 验证会议列表为每条会议返回可直接访问的音频 URL。
//
// 同时校验该 URL 通过静态路由确实可访问（返回 200）。
func (s *MeetingTestSuite) TestMeetingListReturnsAccessibleAudioURL() {
	// 1. 创建会议
	createResp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting",
		strings.NewReader(`{"name":"音频URL会议","participants":"张三"}`))
	s.Require().NoError(err)
	s.Require().NotNil(createResp)
	createResp.AssertOk()

	var created struct {
		Code int `json:"code"`
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	s.Require().NoError(createResp.Bind(&created))
	s.Require().Equal(0, created.Code)
	s.Require().NotZero(created.Data.ID)

	// 2. 模拟归档任务：写入 audio_file_path 与音频文件
	filename := "tdd-audio-test.wav"
	_, err = facades.Orm().Query().
		Model(&models.Meeting{}).
		Where("id = ?", created.Data.ID).
		Update("audio_file_path", filename)
	s.Require().NoError(err)

	disk := facades.Storage().Disk(facades.Config().GetString("audio.storage.disk", "audio"))
	s.Require().NoError(disk.Put(filename, "RIFFfake-wav-content"))
	defer func() { _ = disk.Delete(filename) }()

	// 3. 列表请求
	listResp, err := s.Http(s.T()).WithToken(s.token).Get("/api/meeting?keyword=音频URL会议")
	s.Require().NoError(err)
	s.Require().NotNil(listResp)
	listResp.AssertOk()

	var list struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				ID       uint   `json:"id"`
				AudioURL string `json:"audio_url"`
			} `json:"items"`
		} `json:"data"`
	}
	s.Require().NoError(listResp.Bind(&list))
	s.Require().Equal(0, list.Code)

	// 4. 断言对应会议携带正确的音频 URL（含端口，依据 .env 的 APP_URL）
	expectedBase := facades.Config().Env("APP_URL", "http://localhost").(string)
	expectedURL := strings.TrimRight(expectedBase, "/") + "/audio/" + filename

	var target *struct {
		ID       uint   `json:"id"`
		AudioURL string `json:"audio_url"`
	}
	for i := range list.Data.Items {
		if list.Data.Items[i].ID == created.Data.ID {
			target = &list.Data.Items[i]
			break
		}
	}
	s.Require().NotNil(target, "created meeting should appear in list")
	s.Require().Equal(expectedURL, target.AudioURL,
		"audio_url should be directly accessible absolute URL including port")

	// 5. 验证 URL 可直接访问（静态路由已注册）
	audioResp, err := s.Http(s.T()).Get("/audio/" + filename)
	s.Require().NoError(err)
	s.Require().NotNil(audioResp)
	audioResp.AssertOk()
}
