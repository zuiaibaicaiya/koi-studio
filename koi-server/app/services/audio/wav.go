package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// wavHeaderSize 标准 PCM WAV 头长度。
	wavHeaderSize = 44
	// bitsPerSample 采样位宽，与客户端上行的 16bit PCM 保持一致。
	bitsPerSample = 16
	// numChannels 声道数，转写模型要求单声道。
	numChannels = 1
	// bytesPerSample 单个采样点的字节数。
	bytesPerSample = bitsPerSample / 8
	// fullScale 16bit 采样的满量程，用于归一化到 [-1, 1]。
	fullScale = 32768.0
)

// ErrEmptyPCM 输入的 PCM 数据为空。
var ErrEmptyPCM = errors.New("audio: pcm data is empty")

// PCMToWAV 将裸 PCM（16bit 小端、单声道）封装为 WAV 字节流。
func PCMToWAV(pcm []byte, sampleRate int) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, ErrEmptyPCM
	}
	if sampleRate <= 0 {
		return nil, fmt.Errorf("audio: invalid sample rate %d", sampleRate)
	}

	byteRate := uint32(sampleRate * numChannels * bytesPerSample)
	blockAlign := uint16(numChannels * bytesPerSample)

	buf := bytes.NewBuffer(make([]byte, 0, wavHeaderSize+len(pcm)))

	// RIFF 块
	buf.WriteString("RIFF")
	writeLE(buf, uint32(wavHeaderSize-8+len(pcm)))
	buf.WriteString("WAVE")

	// fmt 子块
	buf.WriteString("fmt ")
	writeLE(buf, uint32(16))
	writeLE(buf, uint16(1)) // PCM
	writeLE(buf, uint16(numChannels))
	writeLE(buf, uint32(sampleRate))
	writeLE(buf, byteRate)
	writeLE(buf, blockAlign)
	writeLE(buf, uint16(bitsPerSample))

	// data 子块
	buf.WriteString("data")
	writeLE(buf, uint32(len(pcm)))
	buf.Write(pcm)

	return buf.Bytes(), nil
}

// PCMToSamples 将 16bit 小端 PCM 解码为 [-1, 1] 区间的 float32 采样点。
//
// dst 用于复用底层数组以避免高频分配；容量不足时会重新分配并返回新切片，
// 调用方应始终使用返回值。
func PCMToSamples(pcm []byte, dst []float32) []float32 {
	count := len(pcm) / bytesPerSample
	if cap(dst) < count {
		dst = make([]float32, count, count*2)
	} else {
		dst = dst[:count]
	}
	for i := range count {
		dst[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*bytesPerSample:]))) / fullScale
	}
	return dst
}

// writeLE 以小端序写入定长数值，写入 bytes.Buffer 不会失败，故忽略错误。
func writeLE(buf *bytes.Buffer, value any) {
	_ = binary.Write(buf, binary.LittleEndian, value)
}

// WAVHeaderInfo WAV 文件头解析结果
type WAVHeaderInfo struct {
	SampleRate   uint32 // 采样率（Hz）
	Channels     uint16 // 声道数
	BitsPerSample uint16 // 位宽
	DataSize     uint32 // PCM 数据字节数
	DurationSec  float64 // 时长（秒）
}

// WAVHeaderMinSize 标准 PCM WAV 头的最小长度
const WAVHeaderMinSize = 44

// ErrNotWAV 文件不是有效的 WAV 格式
var ErrNotWAV = errors.New("audio: not a valid WAV file")

// ParseWAVHeader 解析 WAV 文件头，返回格式信息。
//
// 仅支持 PCM（audioFormat=1）格式的 WAV 文件，支持 44 字节标准头。
// 不读取完整文件内容，仅解析头部即可获取采样率、声道、位宽、时长。
func ParseWAVHeader(data []byte) (*WAVHeaderInfo, error) {
	if len(data) < WAVHeaderMinSize {
		return nil, fmt.Errorf("%w: file too small (%d bytes, need at least %d)", ErrNotWAV, len(data), WAVHeaderMinSize)
	}

	// RIFF 头校验
	if string(data[0:4]) != "RIFF" {
		return nil, fmt.Errorf("%w: missing RIFF marker", ErrNotWAV)
	}
	if string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("%w: missing WAVE marker", ErrNotWAV)
	}

	// fmt 子块校验
	if string(data[12:16]) != "fmt " {
		return nil, fmt.Errorf("%w: missing fmt chunk", ErrNotWAV)
	}

	audioFormat := binary.LittleEndian.Uint16(data[20:22])
	if audioFormat != 1 {
		return nil, fmt.Errorf("%w: unsupported audio format %d (only PCM=1 is supported)", ErrNotWAV, audioFormat)
	}

	info := &WAVHeaderInfo{
		Channels:      binary.LittleEndian.Uint16(data[22:24]),
		SampleRate:    binary.LittleEndian.Uint32(data[24:28]),
		BitsPerSample: binary.LittleEndian.Uint16(data[34:36]),
	}

	if info.SampleRate == 0 {
		return nil, fmt.Errorf("%w: invalid sample rate 0", ErrNotWAV)
	}
	if info.Channels == 0 {
		return nil, fmt.Errorf("%w: invalid channel count 0", ErrNotWAV)
	}
	if info.BitsPerSample == 0 {
		return nil, fmt.Errorf("%w: invalid bits per sample 0", ErrNotWAV)
	}

	// 查找 data 子块（标准头在偏移 36 处，但有些 WAV 可能包含额外块）
	dataSize := uint32(0)
	if len(data) >= 44 && string(data[36:40]) == "data" {
		dataSize = binary.LittleEndian.Uint32(data[40:44])
	} else {
		// 搜索 data 块
		for i := 12; i+8 <= len(data); i++ {
			if string(data[i:i+4]) == "data" {
				dataSize = binary.LittleEndian.Uint32(data[i+4 : i+8])
				break
			}
		}
	}

	// dataSize 为 0 时用文件总大小估算
	if dataSize == 0 && len(data) > WAVHeaderMinSize {
		dataSize = uint32(len(data) - WAVHeaderMinSize)
	}

	info.DataSize = dataSize
	bytesPerSample := uint32(info.BitsPerSample / 8)
	if bytesPerSample > 0 && info.Channels > 0 {
		info.DurationSec = float64(dataSize) / float64(info.SampleRate*uint32(info.Channels)*bytesPerSample)
	}

	return info, nil
}

// IsCompatibleWithTranscription 检查 WAV 格式是否与离线转写模型兼容。
//
// sherpa-onnx 模型要求 16kHz 采样率；单声道最佳，多声道会被自动混音。
// 返回 (compatible, warning) — compatible 为 false 时应拒绝上传，
// warning 非空时提示用户但允许继续（如多声道会被自动混音）。
func (info *WAVHeaderInfo) IsCompatibleWithTranscription() (bool, string) {
	const requiredSampleRate = 16000

	if info.SampleRate != requiredSampleRate {
		return false, fmt.Sprintf("采样率 %d Hz 不兼容，离线转写要求 %d Hz", info.SampleRate, requiredSampleRate)
	}
	if info.BitsPerSample != 16 {
		return false, fmt.Sprintf("位宽 %d bit 不兼容，仅支持 16 bit PCM", info.BitsPerSample)
	}

	warning := ""
	if info.Channels > 1 {
		warning = fmt.Sprintf("音频为 %d 声道，转写时将自动混音为单声道", info.Channels)
	}
	return true, warning
}
