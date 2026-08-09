package api

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"

	"github.com/zishang520/socket.io/v3/pkg/types"
)

// decodeAudioPayload 把 Socket.IO 传来的音频载荷归一化为字节切片。
//
// 不同客户端（浏览器 ArrayBuffer、Node Buffer、Electron TypedArray）经协议
// 解析后落到 Go 侧的具体类型并不一致，这里统一收敛；无法识别的类型直接报错，
// 避免把非音频数据喂给识别器产生无意义的转写结果。
func decodeAudioPayload(value any) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	case *types.BytesBuffer:
		return v.Bytes(), nil
	case *bytes.Buffer:
		return v.Bytes(), nil
	case []int8:
		data := make([]byte, len(v))
		for i, b := range v {
			data[i] = byte(b)
		}
		return data, nil
	case []int16:
		return int16sToBytes(v), nil
	case []uint16:
		data := make([]byte, len(v)*2)
		for i, b := range v {
			binary.LittleEndian.PutUint16(data[i*2:], b)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported audio payload type %T", value)
	}
}

// int16sToBytes 以小端序把 int16 采样点序列化为字节切片。
func int16sToBytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(s))
	}
	return data
}

// decodeAudioFlag 把结束标志归一化为 int。flag 为 0 表示本次会话的最后一帧。
func decodeAudioFlag(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		flag, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("failed to convert flag to int: %w", err)
		}
		return flag, nil
	default:
		return 0, fmt.Errorf("unsupported flag type %T", value)
	}
}
