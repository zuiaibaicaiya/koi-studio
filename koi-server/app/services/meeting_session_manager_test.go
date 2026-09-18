package services

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
)

// MeetingSessionManagerTestSuite 覆盖会议上下文的说话人列表在实时转写期间
// 被动态追加（框选文字注册新说话人）时的行为与并发安全。
type MeetingSessionManagerTestSuite struct {
	suite.Suite
}

func TestMeetingSessionManagerTestSuite(t *testing.T) {
	suite.Run(t, new(MeetingSessionManagerTestSuite))
}

func (s *MeetingSessionManagerTestSuite) TestSpeakerIDListReturnsCopy() {
	ctx := &MeetingContext{MeetingID: 1, SpeakerIDs: []uint{1, 2}}

	list := ctx.SpeakerIDList()
	s.Equal([]uint{1, 2}, list)

	// 返回的是副本：调用方修改不应影响上下文，否则音频工作协程会读到脏数据。
	list[0] = 99
	s.Equal([]uint{1, 2}, ctx.SpeakerIDList())
}

func (s *MeetingSessionManagerTestSuite) TestSpeakerIDListEmpty() {
	ctx := &MeetingContext{MeetingID: 1}

	s.Nil(ctx.SpeakerIDList())
}

func (s *MeetingSessionManagerTestSuite) TestAddSpeakerID() {
	ctx := &MeetingContext{MeetingID: 1}

	s.True(ctx.AddSpeakerID(5))
	s.Equal([]uint{5}, ctx.SpeakerIDList())

	// 重复添加必须被忽略，否则识别时会重复查询同一个说话人。
	s.False(ctx.AddSpeakerID(5))
	s.Equal([]uint{5}, ctx.SpeakerIDList())

	s.False(ctx.AddSpeakerID(0), "零值 ID 不是合法说话人")
	s.Equal([]uint{5}, ctx.SpeakerIDList())

	s.True(ctx.AddSpeakerID(6))
	s.Equal([]uint{5, 6}, ctx.SpeakerIDList())
}

func (s *MeetingSessionManagerTestSuite) TestAddSpeakerIDUpdatesAllContextsOfMeeting() {
	mgr := NewMeetingSessionManager()
	mgr.Bind("client-1", &MeetingContext{MeetingID: 7})
	mgr.Bind("client-2", &MeetingContext{MeetingID: 7, SpeakerIDs: []uint{3}})
	mgr.Bind("client-3", &MeetingContext{MeetingID: 8})

	updated := mgr.AddSpeakerID(7, 11)

	s.Equal(2, updated, "同一会议下的全部客户端上下文都应更新，便于观众端与采集端一致")
	s.Equal([]uint{11}, mgr.Context("client-1").SpeakerIDList())
	s.Equal([]uint{3, 11}, mgr.Context("client-2").SpeakerIDList())
	s.Equal([]uint(nil), mgr.Context("client-3").SpeakerIDList(), "其它会议的上下文不应被影响")

	// 再次调用不会重复追加。
	s.Equal(0, mgr.AddSpeakerID(7, 11))
	s.Equal([]uint{11}, mgr.Context("client-1").SpeakerIDList())
}

func (s *MeetingSessionManagerTestSuite) TestAddSpeakerIDIgnoresInvalidArguments() {
	mgr := NewMeetingSessionManager()
	mgr.Bind("client-1", &MeetingContext{MeetingID: 7})

	s.Equal(0, mgr.AddSpeakerID(0, 11))
	s.Equal(0, mgr.AddSpeakerID(7, 0))
}

// TestSpeakerIDConcurrentAccess 用 -race 验证「转写工作协程读说话人列表」
// 与「Socket.IO 协程动态追加说话人」之间不存在数据竞争。
func (s *MeetingSessionManagerTestSuite) TestSpeakerIDConcurrentAccess() {
	ctx := &MeetingContext{MeetingID: 1, SpeakerIDs: []uint{1}}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				// 每个协程使用互不重叠的 ID 段，避免重复 ID 被去重后计数失真。
				ctx.AddSpeakerID(uint(worker*1000 + j + 2))
				_ = ctx.SpeakerIDList()
			}
		}(i)
	}
	wg.Wait()

	// 8 个协程各追加 200 个互不相同的 ID，全部应被记录。
	s.Len(ctx.SpeakerIDList(), 1+8*200)
}
