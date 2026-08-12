package feature

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"koi-server/tests"
)

// SpeakerIdentifyTestSuite 声纹识别自动化测试，使用 李大爷.wav 进行注册与识别。
type SpeakerIdentifyTestSuite struct {
	suite.Suite
	tests.TestCase
	token    string
	wavPath  string
	speakerID uint
}

func TestSpeakerIdentifyTestSuite(t *testing.T) {
	suite.Run(t, new(SpeakerIdentifyTestSuite))
}

func (s *SpeakerIdentifyTestSuite) SetupSuite() {
	// 注册/登录测试用户
	resp, err := s.Http(s.T()).Post("/api/user/register",
		strings.NewReader(`{"username":"autotest_speaker","password":"test123","nickname":"声纹测试"}`))
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
			strings.NewReader(`{"username":"autotest_speaker","password":"test123"}`))
		s.Require().NoError(err)
		resp.AssertOk()
		s.Require().NoError(resp.Bind(&regResult))
	}

	s.Require().Equal(0, regResult.Code)
	s.Require().NotEmpty(regResult.Data.Token)
	s.token = regResult.Data.Token

	// 定位 WAV 文件（CWD 已由 test_case.go 切换至项目根目录）
	s.wavPath = "李大爷.wav"
	if _, err := os.Stat(s.wavPath); os.IsNotExist(err) {
		// fallback: 从 tests/feature/ 出发的相对路径
		s.wavPath = filepath.Join("..", "..", "李大爷.wav")
	}
}

func (s *SpeakerIdentifyTestSuite) SetupTest()  {}
func (s *SpeakerIdentifyTestSuite) TearDownTest() {}

// ── 模型状态 ──

// TestSpeakerModelStatus 验证声纹模型已正确加载。
func (s *SpeakerIdentifyTestSuite) TestSpeakerModelStatus() {
	resp, err := s.Http(s.T()).WithToken(s.token).Get("/api/speaker/status")
	s.Require().NoError(err)
	resp.AssertOk()

	var result struct {
		Code int `json:"code"`
		Data struct {
			Loaded    bool     `json:"loaded"`
			Dim       int      `json:"dim"`
			Threshold float32  `json:"threshold"`
			Error     string   `json:"error"`
			Speakers  []string `json:"speakers"`
		} `json:"data"`
	}
	s.Require().NoError(resp.Bind(&result))
	s.Equal(0, result.Code)

	s.True(result.Data.Loaded, "声纹模型应已加载，错误: %s", result.Data.Error)
	s.Equal(192, result.Data.Dim, "3dspeaker campplus 特征维度应为 192")
	s.Greater(result.Data.Threshold, float32(0))
	s.T().Logf("声纹模型状态: loaded=%v dim=%d threshold=%.2f 已注册说话人=%v",
		result.Data.Loaded, result.Data.Dim, result.Data.Threshold, result.Data.Speakers)
}

// ── 完整工作流：创建说话人 → 注册声纹 → 1:N 识别 → 1:1 校验 ──

// TestSpeakerFullWorkflow 按顺序执行完整的声纹注册与识别流程。
//
// 执行顺序：创建"李大爷" → 上传声纹音频 → 1:N 识别 → 1:1 校验。
// 不依赖字母序——所有步骤在一个测试方法中顺序执行。
func (s *SpeakerIdentifyTestSuite) TestSpeakerFullWorkflow() {
	// ── 1. 创建说话人（若已存在则查询ID） ──
	s.Run("CreateSpeaker", func() {
		body := `{"name":"李大爷","description":"测试用说话人——李大爷"}`
		resp, err := s.Http(s.T()).WithToken(s.token).Post("/api/speaker", strings.NewReader(body))
		s.Require().NoError(err)
		resp.AssertOk()

		var result struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				ID          uint   `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				AudioCount  int    `json:"audio_count"`
			} `json:"data"`
		}
		s.Require().NoError(resp.Bind(&result))

		if result.Code == 0 {
			s.speakerID = result.Data.ID
			s.T().Logf("✓ 创建说话人: id=%d name=%s", s.speakerID, result.Data.Name)
		} else {
			// 说话人可能已存在（与其他测试套件共享数据库），查询已有ID
			s.T().Logf("说话人创建返回 code=%d msg=%s，尝试查询已有ID", result.Code, result.Msg)
			listResp, err := s.Http(s.T()).WithToken(s.token).Get("/api/speaker")
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
			for _, sp := range listResult.Data.Items {
				if sp.Name == "李大爷" {
					s.speakerID = sp.ID
					break
				}
			}
			s.T().Logf("✓ 复用已有说话人: id=%d", s.speakerID)
		}
		s.Require().NotZero(s.speakerID)
	})

	// ── 2. 上传声纹音频 ──
	s.Run("UploadAudio", func() {
		s.Require().NotZero(s.speakerID, "需要先创建说话人")

		file, err := os.Open(s.wavPath)
		s.Require().NoError(err, "找不到音频文件: %s", s.wavPath)
		defer file.Close()

		body, contentType, err := buildMultipart(file, "李大爷.wav", map[string]string{
			"remark": "注册测试声纹",
		})
		s.Require().NoError(err)

		url := fmt.Sprintf("/api/speaker/%d/audio", s.speakerID)
		resp, err := s.Http(s.T()).
			WithToken(s.token).
			WithHeader("Content-Type", contentType).
			Post(url, body)
		s.Require().NoError(err)
		resp.AssertOk()

		var result struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				ID            uint    `json:"id"`
				SpeakerID     uint    `json:"speaker_id"`
				FileName      string  `json:"file_name"`
				Duration      float64 `json:"duration"`
				ValidDuration float64 `json:"valid_duration"`
				SampleRate    int     `json:"sample_rate"`
				Dim           int     `json:"dim"`
			} `json:"data"`
		}
		s.Require().NoError(resp.Bind(&result))
		s.Equal(0, result.Code, "上传音频失败: %s", result.Msg)
		s.Equal(s.speakerID, result.Data.SpeakerID)
		s.Equal("李大爷.wav", result.Data.FileName)
		s.NotZero(result.Data.Duration)
		s.Greater(result.Data.ValidDuration, float64(4.0), "有效语音应 ≥ 4 秒")
		s.Equal(192, result.Data.Dim)

		s.T().Logf("✓ 注册声纹: audioID=%d duration=%.2fs validDuration=%.2fs dim=%d",
			result.Data.ID, result.Data.Duration, result.Data.ValidDuration, result.Data.Dim)
	})

	// ── 3. 1:N 识别 ──
	s.Run("IdentifySpeaker", func() {
		file, err := os.Open(s.wavPath)
		s.Require().NoError(err, "找不到音频文件: %s", s.wavPath)
		defer file.Close()

		body, contentType, err := buildMultipart(file, "识别测试.wav", nil)
		s.Require().NoError(err)

		resp, err := s.Http(s.T()).
			WithToken(s.token).
			WithHeader("Content-Type", contentType).
			Post("/api/speaker/identify", body)
		s.Require().NoError(err)
		resp.AssertOk()

		var result struct {
			Code int `json:"code"`
			Data struct {
				Matched   bool    `json:"matched"`
				Score     float32 `json:"score"`
				Threshold float32 `json:"threshold"`
				Name      string  `json:"name"`
				Speaker   *struct {
					ID          uint   `json:"id"`
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"speaker"`
			} `json:"data"`
		}
		s.Require().NoError(resp.Bind(&result))
		s.Equal(0, result.Code)

		s.True(result.Data.Matched, "声纹识别应命中")
		s.Equal("李大爷", result.Data.Name, "识别结果应为李大爷")
		s.NotNil(result.Data.Speaker, "应返回完整的说话人对象")
		if result.Data.Speaker != nil {
			s.Equal("李大爷", result.Data.Speaker.Name)
		}
		s.Greater(result.Data.Score, float32(0.5), "相似度应高于阈值 0.5")

		s.T().Logf("✓ 1:N 识别成功: matched=%v name=%s score=%.4f threshold=%.2f",
			result.Data.Matched, result.Data.Name, result.Data.Score, result.Data.Threshold)
	})

	// ── 4. 1:1 校验 ──
	s.Run("VerifySpeaker", func() {
		s.Require().NotZero(s.speakerID, "需要先创建说话人")

		file, err := os.Open(s.wavPath)
		s.Require().NoError(err, "找不到音频文件: %s", s.wavPath)
		defer file.Close()

		body, contentType, err := buildMultipart(file, "校验测试.wav", nil)
		s.Require().NoError(err)

		url := fmt.Sprintf("/api/speaker/%d/verify", s.speakerID)
		resp, err := s.Http(s.T()).
			WithToken(s.token).
			WithHeader("Content-Type", contentType).
			Post(url, body)
		s.Require().NoError(err)
		resp.AssertOk()

		var result struct {
			Code int `json:"code"`
			Data struct {
				Matched   bool    `json:"matched"`
				Score     float32 `json:"score"`
				Threshold float32 `json:"threshold"`
			} `json:"data"`
		}
		s.Require().NoError(resp.Bind(&result))
		s.Equal(0, result.Code)

		s.True(result.Data.Matched, "1:1 校验应命中——音频确实属于李大爷")
		s.Greater(result.Data.Score, float32(0.5), "校验相似度应高于阈值 0.5")

		s.T().Logf("✓ 1:1 校验成功: matched=%v score=%.4f threshold=%.2f",
			result.Data.Matched, result.Data.Score, result.Data.Threshold)
	})
}

// buildMultipart 构造 multipart/form-data 请求体。
func buildMultipart(file *os.File, filename string, extraFields map[string]string) (io.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", err
	}

	for key, val := range extraFields {
		if err := writer.WriteField(key, val); err != nil {
			return nil, "", err
		}
	}

	contentType := writer.FormDataContentType()
	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return &buf, contentType, nil
}
