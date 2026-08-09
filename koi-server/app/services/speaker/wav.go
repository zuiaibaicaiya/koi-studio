package speaker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// WAV 格式常量。
const (
	formatPCM        = 1
	formatIEEEFloat  = 3
	formatExtensible = 0xFFFE

	riffHeaderSize = 12
)

// 音频解析相关错误。
var (
	// ErrEmptyAudio 上传的音频内容为空。
	ErrEmptyAudio = errors.New("speaker: audio data is empty")
	// ErrNotWave 不是合法的 RIFF/WAVE 文件。
	ErrNotWave = errors.New("speaker: not a valid wav file")
	// ErrNoSamples 音频中不含任何采样点。
	ErrNoSamples = errors.New("speaker: wav file contains no samples")
)

// PCM 表示解码后的单声道浮点音频。
type PCM struct {
	// Samples 归一化到 [-1, 1] 的采样点。
	Samples []float32
	// SampleRate 采样率。
	SampleRate int
}

// Duration 返回音频时长（秒）。
func (p PCM) Duration() float64 {
	if p.SampleRate <= 0 {
		return 0
	}

	return float64(len(p.Samples)) / float64(p.SampleRate)
}

// DecodeWAV 解析 WAV 字节流并输出单声道浮点采样。
//
// 支持 PCM 8/16/24/32 位整型与 32/64 位 IEEE 浮点，以及 WAVE_FORMAT_EXTENSIBLE；
// 多声道会按算术平均混音为单声道，因为声纹模型只接受单通道输入。
func DecodeWAV(data []byte) (PCM, error) {
	if len(data) == 0 {
		return PCM{}, ErrEmptyAudio
	}
	if len(data) < riffHeaderSize {
		return PCM{}, ErrNotWave
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return PCM{}, ErrNotWave
	}

	var (
		audioFormat   uint16
		numChannels   uint16
		sampleRate    uint32
		bitsPerSample uint16
		payload       []byte
		gotFmt        bool
	)

	// 逐块遍历，跳过 LIST / fact 等无关块，块长为奇数时需补齐 1 字节对齐。
	for offset := riffHeaderSize; offset+8 <= len(data); {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		body := offset + 8

		if chunkSize < 0 || body > len(data) {
			break
		}
		// 部分录制工具写入的长度字段不准确，这里按实际可用长度兜底。
		end := min(body+chunkSize, len(data))

		switch chunkID {
		case "fmt ":
			if end-body < 16 {
				return PCM{}, fmt.Errorf("speaker: malformed fmt chunk")
			}
			audioFormat = binary.LittleEndian.Uint16(data[body : body+2])
			numChannels = binary.LittleEndian.Uint16(data[body+2 : body+4])
			sampleRate = binary.LittleEndian.Uint32(data[body+4 : body+8])
			bitsPerSample = binary.LittleEndian.Uint16(data[body+14 : body+16])

			// WAVE_FORMAT_EXTENSIBLE 的真实格式记录在扩展字段的 SubFormat GUID 首字段。
			if audioFormat == formatExtensible && end-body >= 26 {
				audioFormat = binary.LittleEndian.Uint16(data[body+24 : body+26])
			}
			gotFmt = true
		case "data":
			payload = data[body:end]
		}

		advance := chunkSize
		if advance%2 == 1 {
			advance++
		}
		offset = body + advance
	}

	if !gotFmt {
		return PCM{}, ErrNotWave
	}
	if len(payload) == 0 {
		return PCM{}, ErrNoSamples
	}
	if numChannels == 0 {
		return PCM{}, fmt.Errorf("speaker: invalid channel count")
	}
	if sampleRate == 0 {
		return PCM{}, fmt.Errorf("speaker: invalid sample rate")
	}

	samples, err := decodeSamples(payload, audioFormat, bitsPerSample)
	if err != nil {
		return PCM{}, err
	}
	if len(samples) == 0 {
		return PCM{}, ErrNoSamples
	}

	return PCM{
		Samples:    downmix(samples, int(numChannels)),
		SampleRate: int(sampleRate),
	}, nil
}

// decodeSamples 按位宽把 data 块解码为交织的浮点采样。
func decodeSamples(payload []byte, audioFormat uint16, bitsPerSample uint16) ([]float32, error) {
	switch audioFormat {
	case formatPCM:
		switch bitsPerSample {
		case 8:
			// 8 位 WAV 为无符号，静音电平是 128。
			out := make([]float32, len(payload))
			for i, b := range payload {
				out[i] = (float32(b) - 128) / 128
			}
			return out, nil
		case 16:
			count := len(payload) / 2
			out := make([]float32, count)
			for i := range count {
				out[i] = float32(int16(binary.LittleEndian.Uint16(payload[i*2:]))) / 32768
			}
			return out, nil
		case 24:
			count := len(payload) / 3
			out := make([]float32, count)
			for i := range count {
				chunk := payload[i*3 : i*3+3]
				value := int32(chunk[0]) | int32(chunk[1])<<8 | int32(chunk[2])<<16
				// 符号位扩展到 32 位。
				if value&0x800000 != 0 {
					value |= ^0xFFFFFF
				}
				out[i] = float32(value) / 8388608
			}
			return out, nil
		case 32:
			count := len(payload) / 4
			out := make([]float32, count)
			for i := range count {
				out[i] = float32(int32(binary.LittleEndian.Uint32(payload[i*4:]))) / 2147483648
			}
			return out, nil
		}
	case formatIEEEFloat:
		switch bitsPerSample {
		case 32:
			count := len(payload) / 4
			out := make([]float32, count)
			for i := range count {
				out[i] = math.Float32frombits(binary.LittleEndian.Uint32(payload[i*4:]))
			}
			return out, nil
		case 64:
			count := len(payload) / 8
			out := make([]float32, count)
			for i := range count {
				out[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(payload[i*8:])))
			}
			return out, nil
		}
	}

	return nil, fmt.Errorf("speaker: unsupported wav format (format=%d, bits=%d)", audioFormat, bitsPerSample)
}

// downmix 把交织的多声道采样按算术平均合并为单声道。
func downmix(interleaved []float32, channels int) []float32 {
	if channels <= 1 {
		return interleaved
	}

	frames := len(interleaved) / channels
	out := make([]float32, frames)
	for i := range frames {
		var sum float32
		for c := range channels {
			sum += interleaved[i*channels+c]
		}
		out[i] = sum / float32(channels)
	}

	return out
}

// Resample 把采样率线性插值转换到 target。
//
// 声纹模型对采样率敏感，必须与训练时保持一致。线性插值在语音场景下
// 足以满足声纹提取精度，且无需引入额外的重采样依赖。
func Resample(pcm PCM, target int) PCM {
	if target <= 0 || pcm.SampleRate == target || len(pcm.Samples) == 0 {
		return pcm
	}

	ratio := float64(target) / float64(pcm.SampleRate)
	count := int(float64(len(pcm.Samples)) * ratio)
	if count <= 0 {
		return PCM{Samples: nil, SampleRate: target}
	}

	out := make([]float32, count)
	last := len(pcm.Samples) - 1
	for i := range count {
		pos := float64(i) / ratio
		left := int(pos)
		if left >= last {
			out[i] = pcm.Samples[last]
			continue
		}
		frac := float32(pos - float64(left))
		out[i] = pcm.Samples[left]*(1-frac) + pcm.Samples[left+1]*frac
	}

	return PCM{Samples: out, SampleRate: target}
}
