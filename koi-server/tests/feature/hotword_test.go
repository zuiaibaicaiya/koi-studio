package feature

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	contractsaudio "koi-server/app/contracts/audio"
	"koi-server/app/facades"
	"koi-server/tests"
)

// HotwordTestSuite 验证实时转写热词的设置与清空语义。
//
// 核心需求：
//   - 会议配置了热词库 → 加载对应热词到识别器
//   - 会议未配置热词库 → 不残留上一场会议的热词
type HotwordTestSuite struct {
	suite.Suite
	tests.TestCase
	token string
}

func TestHotwordTestSuite(t *testing.T) {
	suite.Run(t, new(HotwordTestSuite))
}

func (s *HotwordTestSuite) SetupSuite() {
	// 注册/登录测试用户
	resp, err := s.Http(s.T()).Post("/api/user/register",
		strings.NewReader(`{"username":"autotest_hotword","password":"test123","nickname":"热词测试"}`))
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
		resp, err = s.Http(s.T()).Post("/api/user/login",
			strings.NewReader(`{"username":"autotest_hotword","password":"test123"}`))
		s.Require().NoError(err)
		resp.AssertOk()
		s.Require().NoError(resp.Bind(&regResult))
	}

	s.Require().Equal(0, regResult.Code)
	s.Require().NotEmpty(regResult.Data.Token)
	s.token = regResult.Data.Token
}

// TestSetHotwordsSemantics 验证 SetHotwords 的设置 / 清空 / 不变三种行为。
func (s *HotwordTestSuite) TestSetHotwordsSemantics() {
	// 获取音频服务（模型异步加载）
	audioRaw, err := facades.App().Make(contractsaudio.Binding)
	s.Require().NoError(err)
	audio := audioRaw.(contractsaudio.Transcriber)

	// 等待模型就绪（SetHotwords 的识别器重建需要模型已加载）
	if !audio.Ready() {
		s.T().Log("模型未就绪，等待加载...")
		for i := 0; i < 120; i++ {
			time.Sleep(500 * time.Millisecond)
			if audio.Ready() {
				break
			}
		}
	}
	status := audio.Status()
	s.Require().True(audio.Ready(), "语音识别模型未就绪: loaded=%v error=%s", status.Loaded, status.Error)

	// ── 1. 设置热词（sherpa-onnx 格式：word :weight，权重冒号前缀） ──
	hotwords := "机器学习 :20\n深度学习 :20"
	err = audio.SetHotwords(hotwords, 0)
	s.Require().NoError(err)

	applied, score := audio.Hotwords()
	s.Equal(hotwords, applied, "设置后应返回相同的热词")
	s.Greater(score, float32(0), "热词权重应为正数")
	s.T().Logf("✓ 热词设置成功: %q score=%.1f", applied, score)

	// ── 2. 清空热词（模拟加入无热词库的会议） ──
	err = audio.SetHotwords("", 0)
	s.Require().NoError(err)

	applied, _ = audio.Hotwords()
	s.Empty(applied, "清空后热词应为空，不得残留上一场会议的热词")
	s.T().Log("✓ 热词清空成功: 不残留上一位会议的热词")

	// ── 3. 幂等：再次设置相同热词不报错 ──
	err = audio.SetHotwords(hotwords, 0)
	s.Require().NoError(err)
	err = audio.SetHotwords(hotwords, 0)
	s.Require().NoError(err, "相同热词重复设置不应报错")
	applied, _ = audio.Hotwords()
	s.Equal(hotwords, applied)
	s.T().Log("✓ 热词幂等设置正常")
}

// TestHotwordBuildFromLibrary 验证从热词库构建热词字符串（后端 buildHotwordsString 等效逻辑）。
func (s *HotwordTestSuite) TestHotwordBuildFromLibrary() {
	// ── 1. 创建热词库（已存在时复用，避免重复运行因名称冲突失败） ──
	libResp, err := s.Http(s.T()).WithToken(s.token).Post("/api/hot-word-library",
		strings.NewReader(`{"name":"测试热词库","description":"实时转写热词测试"}`))
	s.Require().NoError(err)
	libResp.AssertOk()

	var libResult struct {
		Code int `json:"code"`
		Data struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	s.Require().NoError(libResp.Bind(&libResult))

	var libID uint
	if libResult.Code == 0 {
		libID = libResult.Data.ID
	} else {
		// 热词库可能已存在（数据库未清理时重复运行），查询并复用
		s.T().Logf("热词库创建可能已存在 (code=%d)，尝试复用...", libResult.Code)
		listResp, err := s.Http(s.T()).WithToken(s.token).Get("/api/hot-word-library?pageSize=100")
		s.Require().NoError(err)
		listResp.AssertOk()
		var listResult struct {
			Code int `json:"code"`
			Data struct {
				Items []struct {
					ID   uint   `json:"id"`
					Name string `json:"name"`
				} `json:"items"`
			} `json:"data"`
		}
		s.Require().NoError(listResp.Bind(&listResult))
		for _, lib := range listResult.Data.Items {
			if lib.Name == "测试热词库" {
				libID = lib.ID
				break
			}
		}
	}
	s.Require().NotZero(libID, "未能创建或复用热词库")
	s.T().Logf("热词库: id=%d name=%s", libID, "测试热词库")

	// ── 2. 添加热词 ──
	for _, word := range []string{"机器学习", "深度学习", "神经网络"} {
		body := fmt.Sprintf(`{"word":%q,"weight":20}`, word)
		wResp, err := s.Http(s.T()).WithToken(s.token).
			Post(fmt.Sprintf("/api/hot-word-library/%d/word", libID), strings.NewReader(body))
		s.Require().NoError(err)
		wResp.AssertOk()
		var wResult struct {
			Code int `json:"code"`
		}
		s.Require().NoError(wResp.Bind(&wResult))
		if wResult.Code != 0 {
			s.T().Logf("热词 %q 可能已存在 (code=%d)，继续", word, wResult.Code)
		}
	}

	// ── 3. 查询热词，验证可加载 ──
	wordResp, err := s.Http(s.T()).WithToken(s.token).
		Get(fmt.Sprintf("/api/hot-word-library/%d/word?pageSize=100", libID))
	s.Require().NoError(err)
	wordResp.AssertOk()

	var wordResult struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				Word   string `json:"word"`
				Weight int    `json:"weight"`
			} `json:"items"`
		} `json:"data"`
	}
	s.Require().NoError(wordResp.Bind(&wordResult))
	s.Require().Equal(0, wordResult.Code)
	s.Require().NotEmpty(wordResult.Data.Items, "热词库中应有热词")

	for _, item := range wordResult.Data.Items {
		s.T().Logf("  热词: %q weight=%d", item.Word, item.Weight)
	}
	s.Require().GreaterOrEqual(len(wordResult.Data.Items), 3, "应至少包含 3 个热词（重复运行时可能多于3个）")
	s.T().Logf("✓ 热词库加载正常: %d 个热词可被后端加载", len(wordResult.Data.Items))

	// ── 4. 创建会议并关联热词库 ──
	meetingBody := fmt.Sprintf(`{"name":"热词测试会议","hot_word_library_ids":%q}`, fmt.Sprint(libID))
	mResp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting", strings.NewReader(meetingBody))
	s.Require().NoError(err)
	mResp.AssertOk()

	var mResult struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ID                uint   `json:"id"`
			HotWordLibraryIds string `json:"hot_word_library_ids"`
		} `json:"data"`
	}
	s.Require().NoError(mResp.Bind(&mResult))
	if mResult.Code != 0 {
		s.T().Logf("会议创建失败: code=%d msg=%s", mResult.Code, mResult.Msg)
	}
	s.Require().Equal(0, mResult.Code)
	s.Equal(fmt.Sprint(libID), mResult.Data.HotWordLibraryIds,
		"会议应正确关联热词库ID")
	s.T().Logf("✓ 会议关联热词库: meeting_id=%d hot_word_library_ids=%q",
		mResult.Data.ID, mResult.Data.HotWordLibraryIds)
}
