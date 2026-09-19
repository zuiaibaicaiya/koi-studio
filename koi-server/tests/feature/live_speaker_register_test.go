package feature

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	contractsaudio "koi-server/app/contracts/audio"
	"koi-server/app/facades"
	"koi-server/app/models"
	"koi-server/app/services"
	"koi-server/tests"
)

// registerSpeakerName 本用例动态注册的说话人名称。
const registerSpeakerName = "动态注册测试说话人"

// LiveSpeakerRegisterTestSuite 覆盖完整链路：
//
//	实时转写（会议内尚无任何说话人）→ 框选转写片段 → 动态注册说话人
//	→ 声纹落库 / 会议说话人列表更新 / 历史归属回填 → 后续转写正确识别该说话人
//
// 与 Socket.IO 层解耦：直接调用转写服务（与 socketio_controller 调用的是同一条链路），
// 框选区间取自数据库中的真实转写记录，等价于前端把框选到的 start_ms/end_ms 上报给后端。
type LiveSpeakerRegisterTestSuite struct {
	suite.Suite
	tests.TestCase
	token     string
	wavPath   string
	speakerID uint
}

func TestLiveSpeakerRegisterTestSuite(t *testing.T) {
	suite.Run(t, new(LiveSpeakerRegisterTestSuite))
}

func (s *LiveSpeakerRegisterTestSuite) SetupSuite() {
	resp, err := s.Http(s.T()).Post("/api/user/register",
		strings.NewReader(`{"username":"autotest_live_speaker","password":"test123","nickname":"动态说话人测试"}`))
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
			strings.NewReader(`{"username":"autotest_live_speaker","password":"test123"}`))
		s.Require().NoError(err)
		resp.AssertOk()
		s.Require().NoError(resp.Bind(&regResult))
	}

	s.Require().Equal(0, regResult.Code)
	s.token = regResult.Data.Token
	s.Require().NotEmpty(s.token)

	s.wavPath = resolveRealtimeWav()
}

// resolveRealtimeWav 选择本用例使用的 16kHz 单声道 WAV 素材。
//
// 优先使用模型自带的测试音频，而不是声纹识别用例所用的 李大爷.wav：
// 该素材的发声人与共享数据库里已注册的声纹不同，因此「新注册的说话人能被
// 正确识别」这一断言不会被全局声纹库中已有的说话人干扰，结果稳定可复现。
func resolveRealtimeWav() string {
	candidates := []string{
		"models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/test_wavs/0.wav",
		"李大爷.wav",
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// TestRegisterSpeakerFromSegmentAndIdentifyAfterwards 覆盖主流程。
func (s *LiveSpeakerRegisterTestSuite) TestRegisterSpeakerFromSegmentAndIdentifyAfterwards() {
	if s.wavPath == "" {
		s.T().Skip("缺少本地测试音频素材（*.wav 不入库），跳过本用例")
	}

	pcmData, err := readWAVPCM(s.wavPath)
	s.Require().NoError(err, "读取音频失败: %s", s.wavPath)
	s.Require().NotEmpty(pcmData)

	audioRaw, err := facades.App().Make(contractsaudio.Binding)
	s.Require().NoError(err)
	audio := audioRaw.(contractsaudio.Transcriber)

	sessionMgrRaw, err := facades.App().Make("meeting.session_manager")
	s.Require().NoError(err)
	sessionMgr := sessionMgrRaw.(*services.MeetingSessionManager)

	s.waitModelReady(audio)

	// ── 1. 会议内不关联任何说话人：模拟「会议开始前还没登记谁在说话」 ──
	meetingID := s.createMeetingWithoutSpeakers()

	clientID := "test-live-speaker-register-001"
	sessionMgr.Bind(clientID, &services.MeetingContext{
		MeetingID:      meetingID,
		AudioStartTime: time.Now(),
	})
	defer sessionMgr.Unbind(clientID)
	defer audio.Release(clientID)

	// ── 2. 推送前半段音频 + 尾部静音（触发断句提交） ──
	half := len(pcmData) / 2
	s.pushPCM(audio, clientID, pcmData[:half], false)
	s.pushPCM(audio, clientID, make([]byte, 16000*2*2), false) // 2 秒静音，强制断句

	before := s.waitForTranscripts(meetingID, 1, 40*time.Second)
	s.Require().NotEmpty(before, "会议尚无任何说话人时也应产出转写记录")

	for _, item := range before {
		s.Equal("未知说话人", item.SpeakerName, "未注册任何说话人时不应识别出具体人")
		s.Nil(item.SpeakerID, "未识别时 speaker_id 应为空")
	}

	// ── 3. 模拟用户框选：取已定稿片段的整体时间范围 ──
	startMs, endMs := timeRange(before)
	s.Require().Greater(endMs-startMs, int64(1000), "框选时长应超过协议下限")

	selectedText := joinText(before)
	s.T().Logf("框选区间: [%dms, %dms), 文本=%q", startMs, endMs, selectedText)

	// ── 4. 动态注册说话人 ──
	result, err := audio.RegisterSpeakerFromSegment(clientID, contractsaudio.SpeakerRegistration{
		MeetingID: meetingID,
		StartMs:   startMs,
		EndMs:     endMs,
		Name:      registerSpeakerName,
		Text:      selectedText,
	})
	s.Require().NoError(err, "从框选片段动态注册说话人失败")
	s.speakerID = result.SpeakerID

	s.NotZero(result.SpeakerID, "应创建说话人")
	s.Equal(registerSpeakerName, result.SpeakerName)
	s.Equal(meetingID, result.MeetingID)
	s.Equal(startMs, result.StartMs)
	s.Equal(endMs, result.EndMs)
	s.Greater(result.ValidDuration, float64(1.0), "参与注册的有效语音应达到实时注册下限")
	s.GreaterOrEqual(result.RelabeledCount, int64(len(before)), "框选区间内的历史转写应全部回填归属")

	// 清理：删除动态注册的说话人，避免污染共享声纹库而影响其它用例。
	s.T().Cleanup(func() { s.deleteSpeaker() })

	// ── 5. 声纹已落库 ──
	speakerService := services.NewSpeakerService()
	speaker, err := speakerService.GetSpeakerById(int(result.SpeakerID))
	s.Require().NoError(err, "动态注册的说话人应已写入数据库")
	s.Equal(registerSpeakerName, speaker.Name)
	s.GreaterOrEqual(speaker.AudioCount, 1, "应至少写入一条声纹音频")
	s.Equal(192, speaker.EmbeddingDim, "3dspeaker campplus 特征维度应为 192")
	s.T().Logf("说话人落库: id=%d name=%s audioCount=%d dim=%d",
		speaker.ID, speaker.Name, speaker.AudioCount, speaker.EmbeddingDim)

	// ── 6. 会议说话人列表：内存上下文立即生效 + 数据库持久化 ──
	ctx := sessionMgr.Context(clientID)
	s.Require().NotNil(ctx)
	s.Contains(ctx.SpeakerIDList(), result.SpeakerID, "内存上下文应包含新说话人，后续断句才能参与比对")

	meeting, err := services.NewMeetingService().GetMeetingById(int(meetingID))
	s.Require().NoError(err)
	s.Contains(idList(meeting.SpeakerIds), strconv.FormatUint(uint64(result.SpeakerID), 10),
		"会议应持久化新说话人，重连后仍可识别")

	// ── 7. 框选区间内的历史转写已重新归属 ──
	relabeled := s.waitForRelabel(meetingID, startMs, endMs, result.SpeakerID)
	s.GreaterOrEqual(relabeled, len(before), "框选区间内的历史转写应全部改判为新说话人")

	// ── 8. 后续实时转写：应识别并区分出这个新添加的说话人 ──
	s.pushPCM(audio, clientID, pcmData[half:], true)

	identified := s.waitForIdentification(meetingID, endMs, result.SpeakerID, 40*time.Second)
	s.Greater(identified, 0,
		"动态注册后，框选区间之后的转写应识别为「%s」而不是未知说话人", registerSpeakerName)

	s.T().Logf("后续转写识别统计: 注册后新增识别到「%s」的记录 %d 条", registerSpeakerName, identified)
}

// ── 断言辅助 ──

// waitModelReady 等待语音模型加载完成。
func (s *LiveSpeakerRegisterTestSuite) waitModelReady(audio contractsaudio.Transcriber) {
	if audio.Ready() {
		return
	}

	s.T().Log("语音模型加载中，等待就绪...")
	for i := 0; i < 120; i++ {
		time.Sleep(500 * time.Millisecond)
		if audio.Ready() {
			return
		}
	}

	status := audio.Status()
	s.Require().True(audio.Ready(), "语音识别模型未就绪: loaded=%v error=%s", status.Loaded, status.Error)
}

// pushPCM 按 100ms 分片推送 PCM，模拟 Socket.IO 客户端上行（每帧 3200 字节）。
func (s *LiveSpeakerRegisterTestSuite) pushPCM(audio contractsaudio.Transcriber, clientID string, pcm []byte, last bool) {
	const chunkSize = 3200

	for i := 0; i < len(pcm); i += chunkSize {
		end := min(i+chunkSize, len(pcm))

		flag := 1
		if last && end == len(pcm) {
			flag = 0 // 结束帧：触发收尾与最后一句提交
		}

		s.Require().NoError(audio.Push(clientID, pcm[i:end], flag))
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForTranscripts 轮询等待会议下已定稿的转写达到至少 count 条。
func (s *LiveSpeakerRegisterTestSuite) waitForTranscripts(meetingID uint, count int, timeout time.Duration) []models.MeetingTranscript {
	deadline := time.Now().Add(timeout)

	var transcripts []models.MeetingTranscript
	for time.Now().Before(deadline) {
		transcripts = s.loadTranscripts(meetingID)
		if len(transcripts) >= count {
			return transcripts
		}
		time.Sleep(500 * time.Millisecond)
	}

	return transcripts
}

// waitForRelabel 轮询等待框选区间内的转写全部改判为指定说话人，返回改判条数。
func (s *LiveSpeakerRegisterTestSuite) waitForRelabel(meetingID uint, startMs, endMs int64, speakerID uint) int {
	deadline := time.Now().Add(10 * time.Second)

	for {
		count := 0
		for _, item := range s.loadTranscripts(meetingID) {
			if item.EndMs <= startMs || item.StartMs >= endMs {
				continue
			}
			s.Require().NotNil(item.SpeakerID, "框选区间内的转写应已归属到新说话人")
			s.Equal(speakerID, *item.SpeakerID)
			s.Equal(registerSpeakerName, item.SpeakerName)
			count++
		}

		if count > 0 || time.Now().After(deadline) {
			return count
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitForIdentification 轮询等待「框选区间之后」的转写被识别为指定说话人。
func (s *LiveSpeakerRegisterTestSuite) waitForIdentification(meetingID uint, afterMs int64, speakerID uint, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)

	identified := 0
	for time.Now().Before(deadline) {
		identified = 0
		for _, item := range s.loadTranscripts(meetingID) {
			if item.StartMs < afterMs {
				continue
			}
			if item.SpeakerID != nil && *item.SpeakerID == speakerID {
				s.Equal(registerSpeakerName, item.SpeakerName)
				identified++
			}
		}
		if identified > 0 {
			return identified
		}
		time.Sleep(500 * time.Millisecond)
	}

	return identified
}

// loadTranscripts 读取会议下已定稿的转写（按 start_ms 升序）。
func (s *LiveSpeakerRegisterTestSuite) loadTranscripts(meetingID uint) []models.MeetingTranscript {
	transcripts, _, err := services.NewMeetingTranscriptService().GetByMeetingID(meetingID, 1, 200)
	s.Require().NoError(err)

	return transcripts
}

// ── 会议 / 说话人准备与清理 ──

// createMeetingWithoutSpeakers 创建一场未关联任何说话人的实时会议。
func (s *LiveSpeakerRegisterTestSuite) createMeetingWithoutSpeakers() uint {
	resp, err := s.Http(s.T()).WithToken(s.token).Post("/api/meeting",
		strings.NewReader(`{"name":"动态注册说话人测试会议","participants":"测试","speaker_ids":""}`))
	s.Require().NoError(err)
	resp.AssertOk()

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	s.Require().NoError(resp.Bind(&result))
	s.Require().Equal(0, result.Code, "创建会议失败: %s", result.Msg)
	s.Require().NotZero(result.Data.ID)

	s.T().Cleanup(func() {
		delResp, delErr := s.Http(s.T()).WithToken(s.token).
			Delete(fmt.Sprintf("/api/meeting/%d", result.Data.ID), strings.NewReader(""))
		if delErr != nil {
			s.T().Logf("清理测试会议失败: %v", delErr)
			return
		}
		delResp.AssertOk()
	})

	return result.Data.ID
}

// deleteSpeaker 删除动态注册的说话人，同时从内存声纹库中注销。
func (s *LiveSpeakerRegisterTestSuite) deleteSpeaker() {
	if s.speakerID == 0 {
		return
	}

	resp, err := s.Http(s.T()).WithToken(s.token).
		Delete(fmt.Sprintf("/api/speaker/%d", s.speakerID), strings.NewReader(""))
	if err != nil {
		s.T().Logf("清理动态注册的说话人失败: %v", err)

		return
	}
	resp.AssertOk()
	s.T().Logf("已清理动态注册的说话人: id=%d", s.speakerID)
}

// ── 纯函数辅助 ──

// timeRange 返回一组转写记录的最小起点与最大终点。
func timeRange(transcripts []models.MeetingTranscript) (int64, int64) {
	start, end := transcripts[0].StartMs, transcripts[0].EndMs
	for _, item := range transcripts {
		start = min(start, item.StartMs)
		end = max(end, item.EndMs)
	}

	return start, end
}

// joinText 把多条转写文本拼接为框选内容（等价于用户跨段选中文字）。
func joinText(transcripts []models.MeetingTranscript) string {
	var builder strings.Builder
	for _, item := range transcripts {
		builder.WriteString(item.Text)
	}

	return builder.String()
}

// idList 拆分逗号分隔的 ID 字符串，便于断言会议说话人列表。
func idList(ids string) []string {
	parts := strings.Split(ids, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}

	return result
}
