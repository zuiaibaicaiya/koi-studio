package config

import (
	"koi-server/app/facades"
)

func init() {
	config := facades.Config()
	config.Add("audio", map[string]any{
		// Speech Model
		//
		// 流式语音识别模型（sherpa-onnx streaming zipformer）的加载参数。
		// dir 为模型目录，其下需包含 encoder/decoder/joiner 三个 onnx 文件、
		// tokens.txt 以及可选的 hotwords.txt。
		"model": map[string]any{
			"dir":              config.Env("AUDIO_MODEL_DIR", "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"),
			"encoder":          config.Env("AUDIO_MODEL_ENCODER", "encoder-epoch-99-avg-1.onnx"),
			"decoder":          config.Env("AUDIO_MODEL_DECODER", "decoder-epoch-99-avg-1.onnx"),
			"joiner":           config.Env("AUDIO_MODEL_JOINER", "joiner-epoch-99-avg-1.onnx"),
			"tokens":           config.Env("AUDIO_MODEL_TOKENS", "tokens.txt"),
			"hotwords_file":    config.Env("AUDIO_MODEL_HOTWORDS_FILE", "hotwords.txt"),
			"num_threads":      config.Env("AUDIO_MODEL_NUM_THREADS", 2),
			"provider":         config.Env("AUDIO_MODEL_PROVIDER", ""),
			"decoding_method":  config.Env("AUDIO_DECODING_METHOD", "modified_beam_search"),
			"max_active_paths": config.Env("AUDIO_MAX_ACTIVE_PATHS", 4),
			// 首帧音频到达时等待模型加载完成的最长秒数，超时返回错误而非无限阻塞。
			"load_timeout": config.Env("AUDIO_MODEL_LOAD_TIMEOUT", 5),
		},

		// Audio Stream
		//
		// 客户端上行音频流的格式与解码节奏。sample_rate / feature_dim 必须与模型一致。
		"stream": map[string]any{
			"sample_rate": config.Env("AUDIO_SAMPLE_RATE", 16000),
			"feature_dim": config.Env("AUDIO_FEATURE_DIM", 80),
			// 每个客户端的音频分片缓冲队列长度，队列满时丢弃当前帧以保护事件循环。
			"queue_size": config.Env("AUDIO_QUEUE_SIZE", 64),
			// 每累计多少帧触发一次解码，用于平衡实时性与 CPU 占用。
			"decode_batch": config.Env("AUDIO_DECODE_BATCH", 3),
			// 中间结果最小下发间隔（毫秒），用于限流，降低网络开销。
			"emit_interval": config.Env("AUDIO_EMIT_INTERVAL", 200),
			// 单句最长时长（秒），超过后强制断句，防止长时间无端点导致结果不下发。
			"max_utterance": config.Env("AUDIO_MAX_UTTERANCE", 20),
		},

		// Endpoint Detection
		//
		// 端点检测规则，决定何时认为一句话结束并输出最终结果。
		"endpoint": map[string]any{
			"enable": config.Env("AUDIO_ENDPOINT_ENABLE", true),
			// 检测到尾部静音超过该秒数且已识别出内容时断句。
			"rule1_min_trailing_silence": config.Env("AUDIO_ENDPOINT_RULE1", 0.5),
			// 检测到尾部静音超过该秒数（无论是否识别出内容）时断句。
			"rule2_min_trailing_silence": config.Env("AUDIO_ENDPOINT_RULE2", 1.0),
			// 语音段超过该秒数时强制断句。
			"rule3_min_utterance_length": config.Env("AUDIO_ENDPOINT_RULE3", 15.0),
		},

		// Hotwords
		//
		// 热词默认权重，客户端可通过 set_hotwords 事件动态覆盖。
		"hotwords": map[string]any{
			"score": config.Env("AUDIO_HOTWORDS_SCORE", 2.0),
		},

		// Recording Storage
		//
		// 会话录音的落盘方式。disk 需在 config/filesystems.go 的 disks 中定义。
		// 归档通过队列异步执行，connection 为空时使用 queue.default 连接。
		"storage": map[string]any{
			"disk":       config.Env("AUDIO_DISK", "audio"),
			"connection": config.Env("AUDIO_ARCHIVE_QUEUE_CONNECTION", ""),
			"queue":      config.Env("AUDIO_ARCHIVE_QUEUE", ""),
		},

		// Session Cleanup
		//
		// 由调度任务（bootstrap/schedule.go）周期性回收空闲会话，
		// 防止客户端异常断线时残留识别流、临时文件与工作协程。
		"cleanup": map[string]any{
			// 会话空闲超过该分钟数即被回收。
			"idle_timeout": config.Env("AUDIO_IDLE_TIMEOUT", 10),
		},
	})
}
