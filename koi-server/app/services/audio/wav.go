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
