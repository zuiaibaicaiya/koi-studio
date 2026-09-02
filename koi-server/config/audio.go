package config

import (
	"koi-server/app/facades"
)

func init() {
	config := facades.Config()
	config.Add("audio", map[string]any{
		// Speech Model (Streaming)
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

		// Offline ASR Model (Non-streaming)
		//
		// 离线语音识别模型（sherpa-onnx offline transducer zipformer）的加载参数。
		// 用于上传音频文件后的批量转写。
		"offline_model": map[string]any{
			// 离线模型类型：zipformer_ctc | transducer | paraformer
			"model_type": config.Env("OFFLINE_MODEL_TYPE", "transducer"),
			"dir":        config.Env("OFFLINE_MODEL_DIR", "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"),
			"model":      config.Env("OFFLINE_MODEL_FILE", "model.onnx"),
			// transducer 类型模型专用（三文件）。
			// 复用与流式同一个 bilingual zipformer 模型，离线批量转写与重新转写均走此模型。
			"encoder": config.Env("OFFLINE_MODEL_ENCODER", "encoder-epoch-99-avg-1.onnx"),
			"decoder": config.Env("OFFLINE_MODEL_DECODER", "decoder-epoch-99-avg-1.onnx"),
			"joiner":  config.Env("OFFLINE_MODEL_JOINER", "joiner-epoch-99-avg-1.onnx"),
			"tokens":  config.Env("OFFLINE_MODEL_TOKENS", "tokens.txt"),
			// bilingual 模型使用 BPE 建模单元，需指定 bpe.vocab 才能正确解码。
			"modeling_unit": config.Env("OFFLINE_MODEL_MODELING_UNIT", "bpe"),
			"bpe_vocab":     config.Env("OFFLINE_MODEL_BPE_VOCAB", "bpe.vocab"),
			// bilingual 流式 zipformer 的 fbank 特征维度为 80（与实时流一致）。
			"feature_dim":      config.Env("OFFLINE_MODEL_FEATURE_DIM", 80),
			"num_threads":      config.Env("OFFLINE_MODEL_NUM_THREADS", 4),
			"provider":         config.Env("OFFLINE_MODEL_PROVIDER", ""),
			"decoding_method":  config.Env("OFFLINE_DECODING_METHOD", "greedy_search"),
			"max_active_paths": config.Env("OFFLINE_MAX_ACTIVE_PATHS", 4),
			"hotwords_score":   config.Env("OFFLINE_HOTWORDS_SCORE", 2.0),
			"load_timeout":     config.Env("OFFLINE_MODEL_LOAD_TIMEOUT", 60),
		},

		// Offline VAD（语音活动检测）
		//
		// 重新转写/离线转写时先做语音活动检测，只在静音处切分音频，
		// 使每个识别窗口都落在句子边界上，避免句子被从中间切开。
		// model 指向 silero_vad.onnx；文件不存在时自动退化为自适应能量检测。
		"offline_vad": map[string]any{
			"enabled":              config.Env("OFFLINE_VAD_ENABLED", true),
			"model":                config.Env("OFFLINE_VAD_MODEL", "models/silero_vad.onnx"),
			"threshold":            config.Env("OFFLINE_VAD_THRESHOLD", 0.4),
			"min_silence_duration": config.Env("OFFLINE_VAD_MIN_SILENCE", 0.4),
			"min_speech_duration":  config.Env("OFFLINE_VAD_MIN_SPEECH", 0.25),
			"window_size":          config.Env("OFFLINE_VAD_WINDOW_SIZE", 512),
			"max_speech_duration":  config.Env("OFFLINE_VAD_MAX_SPEECH", 30),
			"num_threads":          config.Env("OFFLINE_VAD_NUM_THREADS", 1),
			"provider":             config.Env("OFFLINE_VAD_PROVIDER", ""),
			"buffer_seconds":       config.Env("OFFLINE_VAD_BUFFER_SECONDS", 60),
			// 后处理：合并间隔、噪声过滤与首尾补齐
			"min_silence_ms": config.Env("OFFLINE_VAD_MIN_SILENCE_MS", 300),
			"min_speech_ms":  config.Env("OFFLINE_VAD_MIN_SPEECH_MS", 150),
			"padding_ms":     config.Env("OFFLINE_VAD_PADDING_MS", 80),
		},

		// Offline Segmentation（音频切分与断句）
		"offline_segment": map[string]any{
			// 单个识别窗口的最大时长（秒）。
			"max_chunk_seconds": config.Env("OFFLINE_MAX_CHUNK_SECONDS", 30),
			// 允许在两个窗口之间切分的最小静音时长（秒）。
			// 静音短于该值时宁可让窗口超长，也不在语音中间切开。
			"min_silence_cut_seconds": config.Env("OFFLINE_MIN_SILENCE_CUT_SECONDS", 0.4),
			// 断句：单句最少字符数（低于该值不切句，避免碎片化）。
			"sentence_min_runes": config.Env("OFFLINE_SENTENCE_MIN_RUNES", 8),
			// 断句：达到该长度后允许在句内标点/停顿处断句。
			"sentence_target_runes": config.Env("OFFLINE_SENTENCE_TARGET_RUNES", 30),
			// 断句：硬上限，超过后强制断句（仍优先挑最优断点）。
			"sentence_hard_max_runes": config.Env("OFFLINE_SENTENCE_HARD_MAX_RUNES", 50),
			// 断句：字间静音超过该毫秒视为可断句的停顿。
			"sentence_pause_ms": config.Env("OFFLINE_SENTENCE_PAUSE_MS", 500),
			// 断句：跨窗口合并碎片时允许的最大静音间隙（毫秒）。
			"sentence_merge_gap_ms": config.Env("OFFLINE_SENTENCE_MERGE_GAP_MS", 250),
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
			"max_utterance": config.Env("AUDIO_MAX_UTTERANCE", 30),
		},

		// Endpoint Detection
		//
		// 端点检测规则，决定何时认为一句话结束并输出最终结果。
		"endpoint": map[string]any{
			"enable": config.Env("AUDIO_ENDPOINT_ENABLE", true),
			// 检测到尾部静音超过该秒数且已识别出内容时断句。
			// 设得稍长以避免正常说话中的自然停顿被误判为断句。
			"rule1_min_trailing_silence": config.Env("AUDIO_ENDPOINT_RULE1", 1.5),
			// 检测到尾部静音超过该秒数（无论是否识别出内容）时断句。
			"rule2_min_trailing_silence": config.Env("AUDIO_ENDPOINT_RULE2", 2.5),
			// 语音段超过该秒数时强制断句。
			"rule3_min_utterance_length": config.Env("AUDIO_ENDPOINT_RULE3", 30.0),
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

		// Speaker Identification
		//
		// 实时转写中的说话人识别参数。在每句结束后提取声纹做 1:N 检索。
		"speaker_identify": map[string]any{
			// 是否启用说话人识别。
			"enabled": config.Env("AUDIO_SPEAKER_IDENTIFY_ENABLED", true),
			// 语音段最短时长（秒），过短时说话人识别准确率低，跳过识别。
			"min_duration": config.Env("AUDIO_SPEAKER_MIN_DURATION", 1.0),
			// PCM 缓冲区最大时长（秒），超限时放弃识别仅入库文本。
			"max_buffer_duration": config.Env("AUDIO_SPEAKER_MAX_BUFFER_DURATION", 30),
		},
	})
}
