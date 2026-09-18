package api

import (
	"errors"
	"fmt"
	"strings"

	contractsaudio "koi-server/app/contracts/audio"
	"koi-server/app/services"
)

// minSpeakerSegmentMs 动态注册说话人允许的最短框选时长（毫秒）。
//
// 过短的片段无法提取稳定声纹，在协议层直接拒绝，避免把无意义的请求
// 传到声纹提取阶段才失败。
const minSpeakerSegmentMs = 1000

// parseSpeakerRegistration 解析 register-speaker 事件的参数。
//
// 入参为单个对象，字段同时兼容下划线（start_ms）与驼峰（startMs）两种风格，
// 以适配不同版本的客户端。返回值第一项为请求标识（用于回执对齐，可空）。
func parseSpeakerRegistration(args []any) (string, contractsaudio.SpeakerRegistration, error) {
	var req contractsaudio.SpeakerRegistration

	if len(args) == 0 {
		return "", req, fmt.Errorf("缺少请求参数")
	}

	options, ok := args[0].(map[string]any)
	if !ok {
		return "", req, fmt.Errorf("请求参数格式不正确")
	}

	requestID := stringOption(options, "request_id", "requestId")

	name := strings.TrimSpace(stringOption(options, "name", "speaker_name", "speakerName"))
	if name == "" {
		return requestID, req, fmt.Errorf("请填写说话人名称")
	}

	startMs := int64Option(options, "start_ms", "startMs")
	endMs := int64Option(options, "end_ms", "endMs")
	if endMs <= startMs {
		return requestID, req, fmt.Errorf("请先框选要归属到该说话人的转写文字")
	}
	if endMs-startMs < minSpeakerSegmentMs {
		return requestID, req, fmt.Errorf(
			"所选片段仅 %.1f 秒，请至少框选 %.0f 秒的转写文字",
			float64(endMs-startMs)/1000, float64(minSpeakerSegmentMs)/1000,
		)
	}

	req = contractsaudio.SpeakerRegistration{
		MeetingID:   uint(int64Option(options, "meeting_id", "meetingId")),
		StartMs:     startMs,
		EndMs:       endMs,
		Name:        name,
		Description: strings.TrimSpace(stringOption(options, "description")),
		Text:        strings.TrimSpace(stringOption(options, "text")),
	}

	return requestID, req, nil
}

// speakerRegistrationErrorMessage 把动态注册说话人的失败原因转换为面向用户的提示文案。
//
// 业务类错误（名称不合法、未加入会议、有效语音不足）直接回显中文原因；
// 其余错误（会话不存在、录音不可用等）统一给出重试提示，内部细节只写日志，
// 避免把实现细节暴露到界面。
func speakerRegistrationErrorMessage(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, contractsaudio.ErrSpeakerNameRequired),
		errors.Is(err, contractsaudio.ErrSpeakerNameTooLong),
		errors.Is(err, contractsaudio.ErrMeetingNotBound),
		errors.Is(err, contractsaudio.ErrMeetingMismatch),
		errors.Is(err, services.ErrValidSpeechTooShort):
		return err.Error()
	case errors.Is(err, contractsaudio.ErrSpeakerRegistrationUnavailable):
		return "声纹模型不可用，暂时无法注册说话人"
	default:
		return "注册说话人失败，请确认会议正在转写后重试"
	}
}

// stringOption 按候选键顺序读取第一个非空字符串字段。
func stringOption(options map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := options[key].(string); ok && value != "" {
			return value
		}
	}

	return ""
}

// int64Option 按候选键顺序读取第一个数值字段，兼容 JSON 解码后的 float64。
func int64Option(options map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := options[key].(type) {
		case float64:
			return int64(value)
		case int64:
			return value
		case int:
			return int64(value)
		}
	}

	return 0
}
