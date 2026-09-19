package audio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	contracts "koi-server/app/contracts/audio"
	"koi-server/app/services"
)

// LiveSpeakerTestSuite 覆盖「框选转写文字 → 动态注册说话人」的校验逻辑与会话音频切片。
//
// 这些用例不加载语音/声纹模型、不访问数据库，因此可在无模型环境下快速回归；
// 端到端行为（声纹入库、后续转写识别）由 tests/feature 下的集成用例覆盖。
type LiveSpeakerTestSuite struct {
	suite.Suite
	sessionMgr *services.MeetingSessionManager
}

func TestLiveSpeakerTestSuite(t *testing.T) {
	suite.Run(t, new(LiveSpeakerTestSuite))
}

func (s *LiveSpeakerTestSuite) SetupTest() {
	s.sessionMgr = services.NewMeetingSessionManager()
}

// newRegistrationService 构造一个仅用于走校验分支的转写服务：
// 除会议会话管理器外不注入任何会真正执行模型推理或数据库访问的依赖。
func (s *LiveSpeakerTestSuite) newRegistrationService() *Service {
	return &Service{
		cfg: Config{SampleRate: 16000}.normalized(),
		deps: Dependencies{
			SessionMgr:        s.sessionMgr,
			SpeakerService:    services.NewSpeakerService(),
			SpeakerVoiceprint: services.NewSpeakerVoiceprintService(),
		},
		sessions: map[string]*session{},
	}
}

// ── RegisterSpeakerFromSegment 入参校验 ──

func (s *LiveSpeakerTestSuite) TestRegisterSpeakerFromSegmentRejectsBlankName() {
	svc := s.newRegistrationService()

	_, err := svc.RegisterSpeakerFromSegment("client-1", contracts.SpeakerRegistration{
		StartMs: 0,
		EndMs:   3000,
		Name:    "   ",
	})

	s.Require().Error(err)
	s.ErrorIs(err, contracts.ErrSpeakerNameRequired)
}

func (s *LiveSpeakerTestSuite) TestRegisterSpeakerFromSegmentRejectsTooLongName() {
	svc := s.newRegistrationService()

	_, err := svc.RegisterSpeakerFromSegment("client-1", contracts.SpeakerRegistration{
		StartMs: 0,
		EndMs:   3000,
		Name:    strings.Repeat("测", contracts.SpeakerNameMaxRunes+1),
	})

	s.Require().Error(err)
	s.ErrorIs(err, contracts.ErrSpeakerNameTooLong)
}

func (s *LiveSpeakerTestSuite) TestRegisterSpeakerFromSegmentRejectsInvalidRange() {
	svc := s.newRegistrationService()

	cases := []struct {
		name           string
		startMs, endMs int64
	}{
		{"等值区间", 3000, 3000},
		{"倒序区间", 5000, 1000},
		{"负向区间", -1000, -500},
	}

	for _, item := range cases {
		s.Run(item.name, func() {
			_, err := svc.RegisterSpeakerFromSegment("client-1", contracts.SpeakerRegistration{
				StartMs: item.startMs,
				EndMs:   item.endMs,
				Name:    "王小明",
			})

			s.Require().Error(err)
			s.ErrorIs(err, ErrInvalidSegmentRange)
		})
	}
}

func (s *LiveSpeakerTestSuite) TestRegisterSpeakerFromSegmentRequiresVoiceprintDependency() {
	svc := &Service{
		cfg:      Config{SampleRate: 16000}.normalized(),
		deps:     Dependencies{SessionMgr: s.sessionMgr, SpeakerService: services.NewSpeakerService()},
		sessions: map[string]*session{},
	}

	_, err := svc.RegisterSpeakerFromSegment("client-1", contracts.SpeakerRegistration{
		StartMs: 0,
		EndMs:   3000,
		Name:    "王小明",
	})

	s.Require().Error(err)
	s.ErrorIs(err, contracts.ErrSpeakerRegistrationUnavailable)
}

func (s *LiveSpeakerTestSuite) TestRegisterSpeakerFromSegmentRequiresMeetingBinding() {
	svc := s.newRegistrationService()

	// 未通过 join-meeting 绑定会议：即使请求里带了 meetingId 也不能注册，
	// 否则会把声纹与转写记录归属到任意会议。
	_, err := svc.RegisterSpeakerFromSegment("client-1", contracts.SpeakerRegistration{
		MeetingID: 99,
		StartMs:   0,
		EndMs:     3000,
		Name:      "王小明",
	})

	s.Require().Error(err)
	s.ErrorIs(err, contracts.ErrMeetingNotBound)
}

func (s *LiveSpeakerTestSuite) TestRegisterSpeakerFromSegmentRejectsMeetingMismatch() {
	svc := s.newRegistrationService()
	s.sessionMgr.Bind("client-1", &services.MeetingContext{MeetingID: 7})

	// 时间戳相对本连接的音频起点，跨会议注册会造成归属与时间轴错配，必须拒绝。
	_, err := svc.RegisterSpeakerFromSegment("client-1", contracts.SpeakerRegistration{
		MeetingID: 99,
		StartMs:   0,
		EndMs:     3000,
		Name:      "王小明",
	})

	s.Require().Error(err)
	s.ErrorIs(err, contracts.ErrMeetingMismatch)
}

func (s *LiveSpeakerTestSuite) TestRegisterSpeakerFromSegmentAcceptsMatchedMeeting() {
	svc := s.newRegistrationService()
	s.sessionMgr.Bind("client-1", &services.MeetingContext{MeetingID: 7})

	// 会议一致时校验放行，继续走到「取音频片段」这一步。
	_, err := svc.RegisterSpeakerFromSegment("client-1", contracts.SpeakerRegistration{
		MeetingID: 7,
		StartMs:   0,
		EndMs:     3000,
		Name:      "王小明",
	})

	s.Require().Error(err)
	s.ErrorIs(err, ErrSessionNotFound)
}

func (s *LiveSpeakerTestSuite) TestRegisterSpeakerFromSegmentRequiresActiveSession() {
	svc := s.newRegistrationService()
	s.sessionMgr.Bind("client-1", &services.MeetingContext{MeetingID: 7})

	_, err := svc.RegisterSpeakerFromSegment("client-1", contracts.SpeakerRegistration{
		StartMs: 0,
		EndMs:   3000,
		Name:    "王小明",
	})

	s.Require().Error(err)
	s.ErrorIs(err, ErrSessionNotFound)
}

// ── SegmentPCM：时间区间 → 字节区间 ──

func (s *LiveSpeakerTestSuite) TestSegmentPCMReturnsRequestedRange() {
	// 1 秒 16bit 单声道 PCM，采样值与其字节偏移一一对应，便于精确断言。
	const sampleRate = 16000
	pcm := make([]byte, sampleRate*2)
	for i := 0; i < sampleRate; i++ {
		pcm[i*2] = byte(i % 251)
		pcm[i*2+1] = byte(i / 251)
	}
	svc := s.newSegmentService(s.writeTempPCM(pcm))

	// 200ms ~ 700ms → 500ms → 8000 采样 → 16000 字节
	got, err := svc.SegmentPCM("client-1", 200, 700)

	s.Require().NoError(err)
	s.Len(got, 16000)
	s.Equal(pcm[6400:22400], got)
}

func (s *LiveSpeakerTestSuite) TestSegmentPCMClampsNegativeStart() {
	svc := s.newSegmentService(s.writeTempPCM(make([]byte, 16000*2)))

	got, err := svc.SegmentPCM("client-1", -500, 100)

	s.Require().NoError(err)
	// 起点被钳制为 0：0 ~ 100ms → 1600 采样 → 3200 字节
	s.Len(got, 3200)
}

func (s *LiveSpeakerTestSuite) TestSegmentPCMReturnsAvailableBytesWhenRangeExceedsRecording() {
	// 录音只有 1 秒，请求 0~5 秒时应返回已写满的部分，而不是报错中断注册流程。
	svc := s.newSegmentService(s.writeTempPCM(make([]byte, 16000*2)))

	got, err := svc.SegmentPCM("client-1", 0, 5000)

	s.Require().NoError(err)
	s.Len(got, 16000*2)
}

func (s *LiveSpeakerTestSuite) TestSegmentPCMRejectsInvalidArguments() {
	cases := []struct {
		name           string
		clientID       string
		startMs, endMs int64
		expectedErr    error
	}{
		{"空客户端", "", 0, 1000, ErrEmptyClientID},
		{"零长度区间", "client-1", 1000, 1000, ErrInvalidSegmentRange},
		{"倒序区间", "client-1", 2000, 1000, ErrInvalidSegmentRange},
		{"会话不存在", "client-x", 0, 1000, ErrSessionNotFound},
	}

	for _, item := range cases {
		s.Run(item.name, func() {
			svc := s.newSegmentService(s.writeTempPCM(make([]byte, 16000*2)))

			_, err := svc.SegmentPCM(item.clientID, item.startMs, item.endMs)

			s.Require().Error(err)
			s.ErrorIs(err, item.expectedErr)
		})
	}
}

func (s *LiveSpeakerTestSuite) TestSegmentPCMRequiresRecordingBuffer() {
	// 会话存在但录音临时文件不可用（存储不可写）时必须明确报错，
	// 而不是返回空音频让后续声纹提取失败于更下游的位置。
	svc := s.newSegmentService(nil)

	_, err := svc.SegmentPCM("client-1", 0, 1000)

	s.Require().Error(err)
	s.ErrorIs(err, ErrRecordingUnavailable)
}

// ── 纯函数：说话人 ID 追加与音频命名 ──

func (s *LiveSpeakerTestSuite) TestAppendSpeakerID() {
	cases := []struct {
		name     string
		ids      string
		id       uint
		expected string
	}{
		{"空列表", "", 3, "3"},
		{"追加到末尾", "1,2", 3, "1,2,3"},
		{"已存在则不重复追加", "1,2", 2, "1,2"},
		{"容忍空白字符", " 1 , 2 ", 2, " 1 , 2 "},
		{"中间缺失则追加", "1,3", 2, "1,3,2"},
		{"零值ID不追加", "1", 0, "1"},
	}

	for _, item := range cases {
		s.Run(item.name, func() {
			s.Equal(item.expected, appendSpeakerID(item.ids, item.id))
		})
	}
}

func (s *LiveSpeakerTestSuite) TestSpeakerAudioFileName() {
	name := speakerAudioFileName("王小明", 65000)

	s.Contains(name, "65.0s")
	s.Contains(name, "王小明")
	s.True(strings.HasSuffix(name, ".wav"))
}

// writeTempPCM 把 PCM 写入测试临时目录下的文件，用例结束时自动关闭。
func (s *LiveSpeakerTestSuite) writeTempPCM(pcm []byte) *os.File {
	path := filepath.Join(s.T().TempDir(), "session.pcm.tmp")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	s.Require().NoError(err)
	_, err = file.Write(pcm)
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = file.Close() })

	return file
}

// newSegmentService 构造只带音频会话表的转写服务，file 为空表示录音不可用。
func (s *LiveSpeakerTestSuite) newSegmentService(file *os.File) *Service {
	return &Service{
		cfg:      Config{SampleRate: 16000}.normalized(),
		sessions: map[string]*session{"client-1": {clientID: "client-1", tempFile: file}},
	}
}
