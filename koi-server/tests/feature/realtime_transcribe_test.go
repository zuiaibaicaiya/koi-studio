package feature

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	contractsaudio "koi-server/app/contracts/audio"
	"koi-server/app/facades"
	"koi-server/app/services"
	"koi-server/tests"
)

// RealtimeTranscribeTestSuite 模拟 Socket.IO 客户端推送 李大爷.wav 进行实时转写与说话人识别。
//
// 本测试不经过网络层，而是直接调用 audio.Push() 注入 PCM 数据，
// 通过 session manager 绑定会议上下文，最后从数据库验证转写记录。
type RealtimeTranscribeTestSuite struct {
	suite.Suite
	tests.TestCase
	token    string
	wavPath  string
	speakerID uint
	meetingID uint
}

func TestRealtimeTranscribeTestSuite(t *testing.T) {
	suite.Run(t, new(RealtimeTranscribeTestSuite))
}

func (s *RealtimeTranscribeTestSuite) SetupSuite() {
	// 注册/登录测试用户
	resp, err := s.Http(s.T()).Post("/api/user/register",
		strings.NewReader(`{"username":"autotest_realtime","password":"test123","nickname":"实时转写测试"}`))
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
			strings.NewReader(`{"username":"autotest_realtime","password":"test123"}`))
		s.Require().NoError(err)
		resp.AssertOk()
		s.Require().NoError(resp.Bind(&regResult))
	}

	s.Require().Equal(0, regResult.Code)
	s.Require().NotEmpty(regResult.Data.Token)
	s.token = regResult.Data.Token

	s.wavPath = "李大爷.wav"
}

func (s *RealtimeTranscribeTestSuite) SetupTest()  {}
func (s *RealtimeTranscribeTestSuite) TearDownTest() {}

// TestRealtimeTranscribe 模拟 Socket.IO 推送音频、转写、说话人识别全流程。
func (s *RealtimeTranscribeTestSuite) TestRealtimeTranscribe() {
	// ── 1. 准备工作：创建说话人 + 注册声纹 + 创建会议 ──
	s.setupSpeakerAndMeeting()

	// ── 2. 读取 PCM 数据 ──
	pcmData, err := readWAVPCM(s.wavPath)
	s.Require().NoError(err, "读取 WAV 文件失败: %s", s.wavPath)
	s.Require().NotEmpty(pcmData, "PCM 数据为空")
	s.T().Logf("WAV 文件加载: %s, PCM 大小=%d 字节, 约 %.2f 秒",
		s.wavPath, len(pcmData), float64(len(pcmData))/32000.0)

	// ── 3. 获取服务 ──
	audioRaw, err := facades.App().Make(contractsaudio.Binding)
	s.Require().NoError(err)
	audio := audioRaw.(contractsaudio.Transcriber)

	sessionMgrRaw, err := facades.App().Make("meeting.session_manager")
	s.Require().NoError(err)
	sessionMgr := sessionMgrRaw.(*services.MeetingSessionManager)

	// ── 4. 预热声纹库 + 绑定会议上下文 ──
	voiceprintSvc := services.NewSpeakerVoiceprintService()
	s.Require().NoError(voiceprintSvc.Warmup())

	clientID := "test-realtime-client-001"
	sessionMgr.Bind(clientID, &services.MeetingContext{
		MeetingID:      s.meetingID,
		SpeakerIDs:     []uint{s.speakerID},
		AudioStartTime: time.Now(),
	})
	defer sessionMgr.Unbind(clientID)

	// ── 5. 等待模型就绪（大模型加载需要时间，最多等 60 秒） ──
	if !audio.Ready() {
		status := audio.Status()
		s.T().Logf("模型尚未就绪: loaded=%v error=%s, 等待加载...", status.Loaded, status.Error)
		for i := 0; i < 120; i++ {
			time.Sleep(500 * time.Millisecond)
			if audio.Ready() {
				break
			}
		}
	}
	status := audio.Status()
	s.Require().True(audio.Ready(), "语音识别模型未就绪: loaded=%v error=%s", status.Loaded, status.Error)

	// ── 6. 模拟 Socket.IO 逐帧推送 PCM（每帧约 100ms = 3200 字节） ──
	const chunkSize = 3200 // 100ms at 16kHz/16bit/mono
	totalChunks := (len(pcmData) + chunkSize - 1) / chunkSize
	s.T().Logf("开始模拟推送: clientID=%s, totalChunks=%d, chunkSize=%d",
		clientID, totalChunks, chunkSize)

	for i := 0; i < len(pcmData); i += chunkSize {
		end := i + chunkSize
		if end > len(pcmData) {
			end = len(pcmData)
		}
		chunk := pcmData[i:end]

		flag := 1 // 非最后一帧
		if end == len(pcmData) {
			flag = 0 // 最后一帧，触发收尾
		}

		if err := audio.Push(clientID, chunk, flag); err != nil {
			s.T().Logf("Push 错误 (chunk %d/%d): %v", i/chunkSize+1, totalChunks, err)
		}

		// 模拟实时间隔，给解码线程处理时间
		time.Sleep(20 * time.Millisecond)
	}

	s.T().Logf("PCM 推送完成，共 %d 帧", totalChunks)

	// ── 7. 等待异步处理完成 ──
	// 转写是异步的（goroutine 解码），最后一帧 flag=0 会触发 finalize，
	// 但 finalize 本身也是异步的。给足够的处理时间。
	s.T().Log("等待转写与说话人识别完成...")
	time.Sleep(5 * time.Second)

	// ── 8. 查询数据库中的转写记录 ──
	transcriptService := services.NewMeetingTranscriptService()
	transcripts, _, err := transcriptService.GetByMeetingID(s.meetingID, 1, 200)
	s.Require().NoError(err)

	s.T().Logf("数据库转写记录数: %d", len(transcripts))
	for i, t := range transcripts {
		s.T().Logf("  [%d] text=%q speaker=%s speakerID=%v startMs=%d endMs=%d",
			i, t.Text, t.SpeakerName, t.SpeakerID, t.StartMs, t.EndMs)
	}

	// ── 9. 断言：数据库正确存储了说话人信息 ──
	s.Require().NotEmpty(transcripts, "应有至少一条转写记录")

	var identifiedCount int
	var totalText strings.Builder
	for _, t := range transcripts {
		totalText.WriteString(t.Text)

		s.NotZero(t.MeetingID, "meeting_id 不应为 0")
		s.Equal(s.meetingID, t.MeetingID, "meeting_id 应与绑定的会议一致")
		s.NotEmpty(t.Text, "转写文本不应为空")
		s.NotZero(t.StartMs, "start_ms 不应为 0")
		s.NotZero(t.EndMs, "end_ms 不应为 0")
		s.True(t.IsFinal, "is_final 应为 true")

		if t.SpeakerName == "李大爷" {
			identifiedCount++
			s.NotNil(t.SpeakerID, "识别到李大爷时 speaker_id 不应为 nil")
			s.Equal(s.speakerID, *t.SpeakerID, "speaker_id 应与注册的说话人ID一致")
			s.T().Logf("✓ DB 说话人存储正确: speaker_name=%q speaker_id=%d meeting_id=%d text=%q",
				t.SpeakerName, *t.SpeakerID, t.MeetingID, t.Text)
		}
	}

	fullText := totalText.String()
	s.NotEmpty(fullText, "应有转写文本输出")

	// 至少有一条记录识别到了李大爷（同一人用自己的音频做声纹识别，得分 1.0，必然命中）
	s.Require().Greater(identifiedCount, 0,
		"至少一条记录应识别到「李大爷」，请检查声纹注册和说话人识别管线")

	// 释放会话
	audio.Release(clientID)
}

// TestRealtimeTranscribeMergedAudio 用 李大爷.wav 注册声纹，merged.wav 模拟推送，
// 验证 merged.wav 中的说话人被正确识别并存储到数据库。
func (s *RealtimeTranscribeTestSuite) TestRealtimeTranscribeMergedAudio() {
	// ── 1. 准备工作：用李大爷.wav 注册声纹，创建会议 ──
	s.setupSpeakerAndMeeting()

	// ── 2. 读取 merged.wav PCM 数据 ──
	mergedPath := "merged.wav"
	pcmData, err := readWAVPCM(mergedPath)
	s.Require().NoError(err, "读取 merged.wav 失败: %s", mergedPath)
	s.Require().NotEmpty(pcmData, "PCM 数据为空")
	s.T().Logf("merged.wav 加载: PCM 大小=%d 字节, 约 %.2f 秒",
		len(pcmData), float64(len(pcmData))/32000.0)

	// ── 3. 获取服务 ──
	audioRaw, err := facades.App().Make(contractsaudio.Binding)
	s.Require().NoError(err)
	audio := audioRaw.(contractsaudio.Transcriber)

	sessionMgrRaw, err := facades.App().Make("meeting.session_manager")
	s.Require().NoError(err)
	sessionMgr := sessionMgrRaw.(*services.MeetingSessionManager)

	// ── 4. 预热声纹库 + 绑定会议上下文 ──
	voiceprintSvc := services.NewSpeakerVoiceprintService()
	s.Require().NoError(voiceprintSvc.Warmup())

	clientID := "test-merged-client-001"
	sessionMgr.Bind(clientID, &services.MeetingContext{
		MeetingID:      s.meetingID,
		SpeakerIDs:     []uint{s.speakerID},
		AudioStartTime: time.Now(),
	})
	defer sessionMgr.Unbind(clientID)

	// ── 5. 等待模型就绪 ──
	if !audio.Ready() {
		s.T().Logf("模型尚未就绪，等待加载...")
		for i := 0; i < 120; i++ {
			time.Sleep(500 * time.Millisecond)
			if audio.Ready() {
				break
			}
		}
	}
	status := audio.Status()
	s.Require().True(audio.Ready(), "语音识别模型未就绪: loaded=%v error=%s", status.Loaded, status.Error)

	// ── 6. 模拟 Socket.IO 逐帧推送 merged.wav PCM ──
	const chunkSize = 3200
	totalChunks := (len(pcmData) + chunkSize - 1) / chunkSize
	s.T().Logf("开始推送 merged.wav: clientID=%s, totalChunks=%d", clientID, totalChunks)

	for i := 0; i < len(pcmData); i += chunkSize {
		end := i + chunkSize
		if end > len(pcmData) {
			end = len(pcmData)
		}
		chunk := pcmData[i:end]

		flag := 1
		if end == len(pcmData) {
			flag = 0
		}

		if err := audio.Push(clientID, chunk, flag); err != nil {
			s.T().Logf("Push 错误 (chunk %d/%d): %v", i/chunkSize+1, totalChunks, err)
		}

		time.Sleep(20 * time.Millisecond)
	}

	s.T().Logf("merged.wav 推送完成，共 %d 帧", totalChunks)

	// ── 7. 等待异步处理完成 ──
	s.T().Log("等待转写与说话人识别完成...")
	time.Sleep(5 * time.Second)

	// ── 8. 查询数据库中的转写记录 ──
	transcriptService := services.NewMeetingTranscriptService()
	transcripts, _, err := transcriptService.GetByMeetingID(s.meetingID, 1, 200)
	s.Require().NoError(err)

	s.T().Logf("数据库转写记录数: %d", len(transcripts))
	for i, t := range transcripts {
		s.T().Logf("  [%d] text=%q speaker=%s speakerID=%v startMs=%d endMs=%d",
			i, t.Text, t.SpeakerName, t.SpeakerID, t.StartMs, t.EndMs)
	}

	// ── 9. 断言 ──
	s.Require().NotEmpty(transcripts, "应有至少一条转写记录")

	var identifiedCount int
	var totalText strings.Builder
	for _, t := range transcripts {
		totalText.WriteString(t.Text)

		s.NotZero(t.MeetingID, "meeting_id 不应为 0")
		s.Equal(s.meetingID, t.MeetingID)
		s.NotEmpty(t.Text, "转写文本不应为空")
		s.NotZero(t.StartMs, "start_ms 不应为 0")
		s.NotZero(t.EndMs, "end_ms 不应为 0")
		s.True(t.IsFinal, "is_final 应为 true")

		if t.SpeakerName == "李大爷" {
			identifiedCount++
			s.NotNil(t.SpeakerID, "识别到李大爷时 speaker_id 不应为 nil")
			s.Equal(s.speakerID, *t.SpeakerID, "speaker_id 应与注册的说话人ID一致")
			s.T().Logf("✓ DB 说话人存储正确: speaker_name=%q speaker_id=%d meeting_id=%d text=%q",
				t.SpeakerName, *t.SpeakerID, t.MeetingID, t.Text)
		}
	}

	fullText := totalText.String()
	s.NotEmpty(fullText, "应有转写文本输出")

	s.Require().Greater(identifiedCount, 0,
		"至少一条记录应识别到「李大爷」，请检查 merged.wav 是否包含李大爷语音")

	// 释放会话
	audio.Release(clientID)
}

// setupSpeakerAndMeeting 创建说话人、上传声纹、创建会议。
func (s *RealtimeTranscribeTestSuite) setupSpeakerAndMeeting() {
	// 创建说话人
	resp, err := s.Http(s.T()).WithToken(s.token).Post("/api/speaker",
		strings.NewReader(`{"name":"李大爷","description":"实时转写测试说话人"}`))
	s.Require().NoError(err)
	resp.AssertOk()

	var speakerResult struct {
		Code int `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	s.Require().NoError(resp.Bind(&speakerResult))
	if speakerResult.Code != 0 {
		s.T().Logf("说话人可能已存在: %s，尝试查询", speakerResult.Msg)
		// 说话人可能已存在，查询出来
		listResp, err := s.Http(s.T()).WithToken(s.token).Get("/api/speaker")
		s.Require().NoError(err)
		listResp.AssertOk()
		var listResult struct {
			Code int `json:"code"`
			Data struct {
				List []struct {
					ID   uint   `json:"id"`
					Name string `json:"name"`
				} `json:"items"`
			} `json:"data"`
		}
		s.Require().NoError(listResp.Bind(&listResult))
		for _, sp := range listResult.Data.List {
			if sp.Name == "李大爷" {
				s.speakerID = sp.ID
				break
			}
		}
	} else {
		s.speakerID = speakerResult.Data.ID
	}
	s.Require().NotZero(s.speakerID, "未能获取说话人ID")
	s.T().Logf("说话人: id=%d", s.speakerID)

	// 上传声纹音频
	file, err := os.Open(s.wavPath)
	s.Require().NoError(err)
	defer file.Close()

	body, contentType, err := buildMultipart(file, "李大爷.wav", map[string]string{"remark": "实时转写测试"})
	s.Require().NoError(err)

	uploadResp, err := s.Http(s.T()).
		WithToken(s.token).
		WithHeader("Content-Type", contentType).
		Post(fmt.Sprintf("/api/speaker/%d/audio", s.speakerID), body)
	s.Require().NoError(err)
	uploadResp.AssertOk()

	var uploadResult struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	s.Require().NoError(uploadResp.Bind(&uploadResult))
	if uploadResult.Code != 0 {
		s.T().Logf("声纹上传: %s (可能已存在，继续)", uploadResult.Msg)
	} else {
		s.T().Log("声纹上传成功")
	}

	// 创建会议（关联说话人）
	meetingBody := fmt.Sprintf(`{"name":"实时转写测试会议","participants":"李大爷","speaker_ids":"%d"}`, s.speakerID)
	meetingResp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting", strings.NewReader(meetingBody))
	s.Require().NoError(err)
	meetingResp.AssertOk()

	var meetingResult struct {
		Code int `json:"code"`
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	s.Require().NoError(meetingResp.Bind(&meetingResult))
	s.Require().Equal(0, meetingResult.Code)
	s.meetingID = meetingResult.Data.ID
	s.T().Logf("会议创建: id=%d", s.meetingID)
}

// readWAVPCM 读取 WAV 文件并返回剥离头部的原始 PCM 数据。
//
// 标准 PCM WAV 头 44 字节，格式为：16kHz / 16bit / 单声道 / 小端序。
func readWAVPCM(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data) < 44 {
		return nil, fmt.Errorf("WAV 文件过短: %d 字节", len(data))
	}

	// 验证 RIFF 头
	if string(data[0:4]) != "RIFF" {
		return nil, fmt.Errorf("不是有效的 WAV 文件")
	}

	// 验证格式: PCM = 1
	if binary.LittleEndian.Uint16(data[20:22]) != 1 {
		return nil, fmt.Errorf("不是 PCM 格式")
	}

	numChannels := binary.LittleEndian.Uint16(data[22:24])
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	bitsPerSample := binary.LittleEndian.Uint16(data[34:36])

	// 跳过非 PCM 数据块，找到 "data" chunk
	var offset int
	for offset = 12; offset < len(data)-8; {
		chunkID := string(data[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(data[offset+4 : offset+8])
		if chunkID == "data" {
			offset += 8
			pcmData := data[offset : offset+int(chunkSize)]
			// 安全截断
			if int(offset)+int(chunkSize) > len(data) {
				pcmData = data[offset:]
			}
			return pcmData, nil
		}
		offset += 8 + int(chunkSize)
	}

	// fallback: 使用标准 44 字节头
	_ = numChannels
	_ = sampleRate
	_ = bitsPerSample
	return data[44:], nil
}
