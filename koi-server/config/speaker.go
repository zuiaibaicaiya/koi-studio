package config

import (
	"koi-server/app/facades"
)

func init() {
	config := facades.Config()
	config.Add("speaker", map[string]any{
		// Speaker Embedding Model
		//
		// 说话人声纹提取模型（sherpa-onnx speaker embedding extractor）的加载参数。
		// dir 为模型目录，file 为其中的 onnx 模型文件名。
		// 可用模型见 https://github.com/k2-fsa/sherpa-onnx/releases/tag/speaker-recongition-models
		"model": map[string]any{
			"dir":         config.Env("SPEAKER_MODEL_DIR", "models/speaker"),
			"file":        config.Env("SPEAKER_MODEL_FILE", "3dspeaker_speech_campplus_sv_zh-cn_16k-common.onnx"),
			"num_threads": config.Env("SPEAKER_MODEL_NUM_THREADS", 2),
			"provider":    config.Env("SPEAKER_MODEL_PROVIDER", ""),
			"debug":       config.Env("SPEAKER_MODEL_DEBUG", false),
			// 调用声纹接口时等待模型加载完成的最长秒数，超时返回错误而非无限阻塞。
			"load_timeout": config.Env("SPEAKER_MODEL_LOAD_TIMEOUT", 10),
		},

		// Voiceprint Search
		//
		// 声纹检索参数。threshold 为余弦相似度阈值，取值区间 (0, 1)：
		// 调高可降低误识率，调低可提升召回率，建议结合实际录音质量调整。
		"search": map[string]any{
			"threshold": config.Env("SPEAKER_SEARCH_THRESHOLD", 0.5),
		},

		// Registration Audio
		//
		// 注册音频的约束。sample_rate 必须与模型训练采样率一致，
		// 上传音频若不匹配会自动混音并线性重采样到该值。
		"audio": map[string]any{
			"sample_rate": config.Env("SPEAKER_AUDIO_SAMPLE_RATE", 16000),
			// 有效音频最短时长（秒），过短的音频无法提取稳定声纹。
			"min_duration": config.Env("SPEAKER_AUDIO_MIN_DURATION", 0.5),
			// 单条音频参与提取的最长时长（秒），超出部分被截断。
			"max_duration": config.Env("SPEAKER_AUDIO_MAX_DURATION", 60.0),
			// 上传文件大小上限（字节）。
			"max_file_size": config.Env("SPEAKER_AUDIO_MAX_FILE_SIZE", 20*1024*1024),
		},

		// Valid Speech
		//
		// 注册说话人时要求的有效语音（去除静音后的实际说话）最短时长（秒）。
		// 低于该值说明语音不足，无法注册出稳定声纹，接口会直接给出提示。
		"min_valid_duration": config.Env("SPEAKER_MIN_VALID_DURATION", 5.0),

		// Voice Activity Detection (VAD)
		//
		// 语音活动检测模型，用于从音频中切出有效语音、剔除静音段，
		// 从而统计“实际说话时长”而非“文件总时长”。模型缺失时退化为
		// 以整段音频时长作为有效语音时长。
		"vad": map[string]any{
			"model":     config.Env("SPEAKER_VAD_MODEL", "models/speaker/silero_vad.onnx"),
			"threshold": config.Env("SPEAKER_VAD_THRESHOLD", 0.5),
		},

		// Voiceprint Storage
		//
		// 注册音频的落盘方式。disk 需在 config/filesystems.go 的 disks 中定义，
		// dir 为该磁盘下存放说话人音频的子目录。
		"storage": map[string]any{
			"disk": config.Env("SPEAKER_DISK", "speaker"),
			"dir":  config.Env("SPEAKER_DIR", "speakers"),
		},
	})
}
