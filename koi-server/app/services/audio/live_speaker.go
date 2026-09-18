package audio

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	contracts "koi-server/app/contracts/audio"
	"koi-server/app/models"
)

// RegisterSpeakerFromSegment 从会话音频的指定时间段动态注册说话人。
//
// 完整链路：
//  1. 解析客户端当前绑定的会议，校验名称与时间段；
//  2. 从会话录音中截取 [StartMs, EndMs) 的 PCM 并封装为 WAV；
//  3. 按名称复用已有说话人（追加声纹）或新建说话人；
//  4. 提取声纹、落盘归档、写入数据库，并立即刷新内存声纹库；
//  5. 把该说话人加入会议的说话人列表（内存上下文 + 数据库）；
//  6. 把该时间段内已定稿的历史转写重新归属到该说话人。
//
// 完成后，后续实时转写即可通过 1:N 声纹检索识别并区分出该说话人。
func (s *Service) RegisterSpeakerFromSegment(clientID string, req contracts.SpeakerRegistration) (contracts.SpeakerRegistrationResult, error) {
	var result contracts.SpeakerRegistrationResult

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return result, contracts.ErrSpeakerNameRequired
	}
	if utf8.RuneCountInString(name) > contracts.SpeakerNameMaxRunes {
		return result, contracts.ErrSpeakerNameTooLong
	}
	if req.StartMs < 0 || req.EndMs <= req.StartMs {
		return result, ErrInvalidSegmentRange
	}
	if s.deps.SpeakerVoiceprint == nil || s.deps.SpeakerService == nil {
		return result, contracts.ErrSpeakerRegistrationUnavailable
	}

	// 会议以客户端实际绑定的上下文（join-meeting）为准：片段时间是相对本连接
	// 音频起点的偏移，若允许请求指定会议，会与目标会议的转写时间轴错配。
	var boundMeetingID uint
	if s.deps.SessionMgr != nil {
		if ctx := s.deps.SessionMgr.Context(clientID); ctx != nil {
			boundMeetingID = ctx.MeetingID
		}
	}
	if boundMeetingID == 0 {
		return result, contracts.ErrMeetingNotBound
	}
	if req.MeetingID != 0 && req.MeetingID != boundMeetingID {
		return result, contracts.ErrMeetingMismatch
	}
	meetingID := boundMeetingID

	pcm, err := s.SegmentPCM(clientID, req.StartMs, req.EndMs)
	if err != nil {
		return result, err
	}
	wavData, err := PCMToWAV(pcm, s.cfg.SampleRate)
	if err != nil {
		return result, err
	}

	// 先预热内存声纹库：确保库内已有会议之前选择的其他说话人，
	// 避免注册新说话人后检索范围不完整（预热仅在该实例首次调用时真正执行）。
	if err := s.deps.SpeakerVoiceprint.Warmup(); err != nil {
		s.deps.Log.Warning(fmt.Sprintf("audio: voiceprint warmup failed before speaker registration: %v", err))
	}

	speaker, created, err := s.resolveSpeaker(name, strings.TrimSpace(req.Description))
	if err != nil {
		return result, err
	}

	remark := strings.TrimSpace(req.Text)
	audio, err := s.deps.SpeakerVoiceprint.RegisterAudioBytes(
		&speaker,
		wavData,
		speakerAudioFileName(name, req.StartMs),
		remark,
	)
	if err != nil {
		// 新建的说话人若声纹注册失败则回滚，避免留下没有声纹的空档案。
		if created {
			if _, delErr := s.deps.SpeakerService.DeleteSpeakerById(int(speaker.ID)); delErr != nil {
				s.deps.Log.Warning(fmt.Sprintf("audio: failed to rollback speaker %d: %v", speaker.ID, delErr))
			}
		}

		return result, err
	}

	// 会议说话人列表：内存上下文立即生效，数据库保证重连后依然生效。
	s.attachSpeakerToMeeting(meetingID, speaker.ID)

	// 历史归属回填：把选中时间段内的转写重新归属到新说话人。
	relabeled := s.relabelTranscripts(meetingID, req.StartMs, req.EndMs, speaker.ID, speaker.Name)

	s.deps.Log.Info(fmt.Sprintf(
		"audio: speaker %q (id=%d) registered from segment [%dms, %dms) for meeting %d, relabeled=%d",
		speaker.Name, speaker.ID, req.StartMs, req.EndMs, meetingID, relabeled,
	))

	return contracts.SpeakerRegistrationResult{
		SpeakerID:          speaker.ID,
		SpeakerName:        speaker.Name,
		SpeakerDescription: speaker.Description,
		MeetingID:          meetingID,
		StartMs:            req.StartMs,
		EndMs:              req.EndMs,
		AudioDuration:      audio.Duration,
		ValidDuration:      audio.ValidDuration,
		RelabeledCount:     relabeled,
	}, nil
}

// resolveSpeaker 按名称复用已有说话人，不存在时新建。
// 返回的第二个值表示本次是否新建了说话人（用于失败回滚）。
func (s *Service) resolveSpeaker(name, description string) (models.Speaker, bool, error) {
	exists, err := s.deps.SpeakerService.IsSpeakerNameExists(name, 0)
	if err != nil {
		return models.Speaker{}, false, err
	}
	if exists {
		// 同名说话人已存在：直接追加一条声纹，避免产生重复档案。
		speaker, err := s.deps.SpeakerService.GetSpeakerByName(name)
		if err != nil {
			return models.Speaker{}, false, err
		}

		return speaker, false, nil
	}

	speaker := models.Speaker{Name: name, Description: description}
	if err := s.deps.SpeakerService.AddSpeaker(&speaker); err != nil {
		return models.Speaker{}, false, err
	}

	return speaker, true, nil
}

// attachSpeakerToMeeting 把说话人加入会议的说话人列表。
func (s *Service) attachSpeakerToMeeting(meetingID, speakerID uint) {
	if meetingID == 0 || speakerID == 0 {
		return
	}

	if s.deps.SessionMgr != nil {
		s.deps.SessionMgr.AddSpeakerID(meetingID, speakerID)
	}
	if s.deps.MeetingService == nil {
		return
	}

	meeting, err := s.deps.MeetingService.GetMeetingById(int(meetingID))
	if err != nil {
		s.deps.Log.Warning(fmt.Sprintf("audio: failed to load meeting %d to attach speaker: %v", meetingID, err))
		return
	}

	updated := appendSpeakerID(meeting.SpeakerIds, speakerID)
	if updated == meeting.SpeakerIds {
		return
	}
	meeting.SpeakerIds = updated
	if err := s.deps.MeetingService.UpdateMeeting(&meeting); err != nil {
		s.deps.Log.Warning(fmt.Sprintf("audio: failed to persist speaker list for meeting %d: %v", meetingID, err))
	}
}

// relabelTranscripts 把时间段内已定稿的转写记录重新归属到目标说话人。
func (s *Service) relabelTranscripts(meetingID uint, startMs, endMs int64, speakerID uint, speakerName string) int64 {
	if s.deps.TranscriptService == nil {
		return 0
	}

	count, err := s.deps.TranscriptService.RelabelByTimeRange(meetingID, startMs, endMs, speakerID, speakerName)
	if err != nil {
		s.deps.Log.Warning(fmt.Sprintf("audio: failed to relabel transcripts for meeting %d: %v", meetingID, err))
		return 0
	}

	return count
}

// appendSpeakerID 把说话人ID追加到逗号分隔的ID字符串，已存在时原样返回。
func appendSpeakerID(ids string, id uint) string {
	if id == 0 {
		return ids
	}

	target := strconv.FormatUint(uint64(id), 10)

	for _, part := range strings.Split(ids, ",") {
		if strings.TrimSpace(part) == target {
			return ids
		}
	}
	if strings.TrimSpace(ids) == "" {
		return target
	}

	return ids + "," + target
}

// speakerAudioFileName 生成动态注册时使用的声纹音频文件名（仅用于展示与回溯）。
func speakerAudioFileName(name string, startMs int64) string {
	return fmt.Sprintf("实时会议-%.1fs-%s.wav", float64(startMs)/1000, name)
}
