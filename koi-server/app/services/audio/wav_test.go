package audio

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/suite"
)

type WAVTestSuite struct {
	suite.Suite
}

func TestWAVTestSuite(t *testing.T) {
	suite.Run(t, new(WAVTestSuite))
}

func (s *WAVTestSuite) TestPCMToWAVRejectsEmptyInput() {
	data, err := PCMToWAV(nil, 16000)

	s.Nil(data)
	s.ErrorIs(err, ErrEmptyPCM)
}

func (s *WAVTestSuite) TestPCMToWAVRejectsInvalidSampleRate() {
	_, err := PCMToWAV([]byte{0x00, 0x01}, 0)

	s.Error(err)
}

func (s *WAVTestSuite) TestPCMToWAVWritesValidHeader() {
	pcm := make([]byte, 320)
	sampleRate := 16000

	wav, err := PCMToWAV(pcm, sampleRate)

	s.Require().NoError(err)
	s.Len(wav, wavHeaderSize+len(pcm))
	s.Equal("RIFF", string(wav[0:4]))
	s.Equal("WAVE", string(wav[8:12]))
	s.Equal("fmt ", string(wav[12:16]))
	s.Equal("data", string(wav[36:40]))

	// RIFF 块长度 = 文件总长 - 8
	s.Equal(uint32(wavHeaderSize-8+len(pcm)), binary.LittleEndian.Uint32(wav[4:8]))
	// PCM 格式、单声道、16bit
	s.Equal(uint16(1), binary.LittleEndian.Uint16(wav[20:22]))
	s.Equal(uint16(numChannels), binary.LittleEndian.Uint16(wav[22:24]))
	s.Equal(uint16(bitsPerSample), binary.LittleEndian.Uint16(wav[34:36]))
	// 采样率与字节率
	s.Equal(uint32(sampleRate), binary.LittleEndian.Uint32(wav[24:28]))
	s.Equal(uint32(sampleRate*numChannels*bytesPerSample), binary.LittleEndian.Uint32(wav[28:32]))
	// data 子块长度
	s.Equal(uint32(len(pcm)), binary.LittleEndian.Uint32(wav[40:44]))
}

func (s *WAVTestSuite) TestPCMToSamplesNormalizesAmplitude() {
	// 依次为 0、最大正值、最小负值
	pcm := []byte{
		0x00, 0x00,
		0xFF, 0x7F,
		0x00, 0x80,
	}

	samples := PCMToSamples(pcm, nil)

	s.Require().Len(samples, 3)
	s.InDelta(0.0, samples[0], 1e-6)
	s.InDelta(0.999969, samples[1], 1e-5)
	s.InDelta(-1.0, samples[2], 1e-6)
}

func (s *WAVTestSuite) TestPCMToSamplesReusesBuffer() {
	buffer := make([]float32, 0, 8)
	pcm := make([]byte, 8)

	samples := PCMToSamples(pcm, buffer)

	s.Len(samples, 4)
	s.Equal(cap(buffer), cap(samples), "容量足够时应复用底层数组，避免高频分配")
}

func (s *WAVTestSuite) TestPCMToSamplesGrowsBufferWhenTooSmall() {
	buffer := make([]float32, 0, 2)
	pcm := make([]byte, 20)

	samples := PCMToSamples(pcm, buffer)

	s.Len(samples, 10)
	s.GreaterOrEqual(cap(samples), 10)
}

func (s *WAVTestSuite) TestPCMToSamplesIgnoresTrailingOddByte() {
	// 半个采样点无法解码，应被丢弃而不是 panic
	samples := PCMToSamples([]byte{0x01, 0x02, 0x03}, nil)

	s.Len(samples, 1)
}
