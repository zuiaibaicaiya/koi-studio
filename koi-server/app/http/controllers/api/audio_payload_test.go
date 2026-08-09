package api

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/zishang520/socket.io/v3/pkg/types"
)

type AudioPayloadTestSuite struct {
	suite.Suite
}

func TestAudioPayloadTestSuite(t *testing.T) {
	suite.Run(t, new(AudioPayloadTestSuite))
}

func (s *AudioPayloadTestSuite) TestDecodeAudioPayloadSupportedTypes() {
	cases := []struct {
		name     string
		input    any
		expected []byte
	}{
		{"bytes", []byte{0x01, 0x02}, []byte{0x01, 0x02}},
		{"string", "ab", []byte{'a', 'b'}},
		{"bytes buffer", bytes.NewBuffer([]byte{0x03}), []byte{0x03}},
		{"types buffer", types.NewBytesBuffer([]byte{0x04}), []byte{0x04}},
		{"int8 slice", []int8{-1, 2}, []byte{0xFF, 0x02}},
		{"int16 slice", []int16{1, -1}, []byte{0x01, 0x00, 0xFF, 0xFF}},
		{"uint16 slice", []uint16{258}, []byte{0x02, 0x01}},
	}

	for _, item := range cases {
		s.Run(item.name, func() {
			data, err := decodeAudioPayload(item.input)

			s.Require().NoError(err)
			s.Equal(item.expected, data)
		})
	}
}

func (s *AudioPayloadTestSuite) TestDecodeAudioPayloadRejectsUnknownType() {
	// 未知类型必须报错，而不是退化成字符串塞给识别器产生噪声结果
	data, err := decodeAudioPayload(struct{}{})

	s.Nil(data)
	s.ErrorContains(err, "unsupported audio payload type")
}

func (s *AudioPayloadTestSuite) TestDecodeAudioFlagSupportedTypes() {
	cases := []struct {
		name     string
		input    any
		expected int
	}{
		{"int", 1, 1},
		{"int64", int64(0), 0},
		{"float64", float64(1), 1},
		{"string", "0", 0},
	}

	for _, item := range cases {
		s.Run(item.name, func() {
			flag, err := decodeAudioFlag(item.input)

			s.Require().NoError(err)
			s.Equal(item.expected, flag)
		})
	}
}

func (s *AudioPayloadTestSuite) TestDecodeAudioFlagRejectsInvalidString() {
	_, err := decodeAudioFlag("not-a-number")

	s.ErrorContains(err, "failed to convert flag to int")
}

func (s *AudioPayloadTestSuite) TestDecodeAudioFlagRejectsUnknownType() {
	_, err := decodeAudioFlag(struct{}{})

	s.ErrorContains(err, "unsupported flag type")
}
