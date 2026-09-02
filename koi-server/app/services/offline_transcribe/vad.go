package offlinetranscribe

import (
	"fmt"
	"math"
	"os"
	"sort"
	"sync"

	"github.com/goravel/framework/contracts/log"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// speechSpan 表示音频中的一段语音，采样下标区间 [Start, End)。
type speechSpan struct {
	Start int
	End   int
}

// length 返回语音段的采样数。
func (s speechSpan) length() int { return s.End - s.Start }

// seconds 返回语音段时长（秒）。
func (s speechSpan) seconds(sampleRate int) float64 {
	if sampleRate <= 0 {
		return 0
	}
	return float64(s.length()) / float64(sampleRate)
}

// vadSampleRate sherpa-onnx Silero VAD 的模型输入采样率。
// 输入音频采样率不同时先线性重采样，再把结果换算回原始采样坐标。
const vadSampleRate = 16000

// 能量法 VAD 的经验参数（与采样率无关）。
const (
	// energyFrameMs 能量分析帧长（毫秒）。20ms 足以区分语音与静音。
	energyFrameMs = 20
	// energyAbsFloor 能量阈值的绝对下限（RMS）。数字静音时底噪为 0，
	// 阈值若完全跟随底噪会退化到 0，把量化噪声误判为语音。
	energyAbsFloor = 0.01
	// energyNoiseFactor 阈值相对底噪的倍数：threshold = max(AbsFloor, 底噪 × NoiseFactor)。
	energyNoiseFactor = 3.0
	// energyPeakRatio 阈值相对语音峰值的上限比例。低信噪比场景下底噪倍数会
	// 算出高于语音本身的阈值，用峰值比例兜底，保证仍能检出语音。
	energyPeakRatio = 0.6
)

// vadPostOptions 语音段的后处理参数，全部为毫秒，与采样率无关。
type vadPostOptions struct {
	// MinSilenceMs 两段语音之间的静音不超过该值时合并为同一段（说话中的自然停顿）。
	MinSilenceMs int
	// MinSpeechMs 短于该时长的语音段视为噪声丢弃；<=0 表示不丢弃。
	MinSpeechMs int
	// PaddingMs 语音段首尾各扩展的时长，避免切掉首尾音节。
	PaddingMs int
}

// vad 封装语音活动检测能力。
//
// 优先使用 sherpa-onnx 的 Silero VAD（模型文件存在且加载成功时）；
// 模型缺失、加载失败或运行异常时自动退化为自适应能量检测，
// 保证「重新转写」流程不因缺少 VAD 模型而中断。
//
// 检测结果是音频中的语音区间，供分块规划与近似时间戳对齐使用：
// 音频只在静音处被切开，句子不会从中间被截断。
type vad struct {
	cfg    Config
	logger log.Log

	once     sync.Once
	detector *sherpa.VoiceActivityDetector
	broken   bool // Silero VAD 运行期发生异常，永久退化为能量检测
}

// newVAD 构造 VAD。logger 允许为空（测试场景）。
func newVAD(cfg Config, logger log.Log) *vad {
	return &vad{cfg: cfg, logger: logger}
}

// Close 释放 Silero VAD 资源，可安全重复调用。
func (v *vad) Close() {
	v.once.Do(func() {})
	if v.detector != nil {
		sherpa.DeleteVoiceActivityDetector(v.detector)
		v.detector = nil
	}
}

// Detect 返回音频中的语音区间（原始采样下标，按起点升序、互不重叠）。
func (v *vad) Detect(samples []float32, sampleRate int) []speechSpan {
	if len(samples) == 0 || sampleRate <= 0 {
		return nil
	}
	opt := v.cfg.vadPostOptions()

	if det := v.ensure(); det != nil {
		if spans, ok := v.detectSilero(det, samples, sampleRate); ok {
			return postProcessSpans(spans, len(samples), sampleRate, opt)
		}
	}
	return detectEnergySpans(samples, sampleRate, opt)
}

// UsesSilero 报告当前是否使用 Silero VAD（false 表示退化为能量检测）。
func (v *vad) UsesSilero() bool {
	return v.ensure() != nil
}

// ensure 惰性加载 Silero VAD，只尝试一次。
func (v *vad) ensure() *sherpa.VoiceActivityDetector {
	v.once.Do(func() {
		if !v.cfg.VadEnabled {
			return
		}
		path := v.cfg.VadModelPath()
		if path == "" {
			return
		}
		if _, err := os.Stat(path); err != nil {
			v.warnf("offline: silero vad model not found at %s, fallback to energy vad (%v)", path, err)
			return
		}

		cfg := sherpa.VadModelConfig{
			SileroVad: sherpa.SileroVadModelConfig{
				Model:              path,
				Threshold:          v.cfg.VadThreshold,
				MinSilenceDuration: v.cfg.VadMinSilenceDuration,
				MinSpeechDuration:  v.cfg.VadMinSpeechDuration,
				WindowSize:         v.cfg.VadWindowSize,
				MaxSpeechDuration:  v.cfg.VadMaxSpeechDuration,
			},
			SampleRate: vadSampleRate,
			NumThreads: v.cfg.VadNumThreads,
			Provider:   v.cfg.VadProvider,
		}
		det := sherpa.NewVoiceActivityDetector(&cfg, v.cfg.VadBufferSeconds)
		if det == nil {
			v.warnf("offline: failed to create silero vad from %s, fallback to energy vad", path)
			return
		}
		v.detector = det
		v.infof("offline: silero vad loaded from %s", path)
	})
	return v.detector
}

// detectSilero 用 Silero VAD 检测语音段。ok 为 false 表示检测不可用（应退化为能量检测）。
//
// C 库在模型异常时可能直接崩溃而非返回错误，这里统一 recover，
// 并把 VAD 标记为不可用，避免后续分块反复触发崩溃。
func (v *vad) detectSilero(det *sherpa.VoiceActivityDetector, samples []float32, sampleRate int) (spans []speechSpan, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			v.errorf("offline: silero vad panicked: %v, disable it and fallback to energy vad", r)
			v.detector = nil
			v.broken = true
			spans, ok = nil, false
		}
	}()
	if v.broken {
		return nil, false
	}

	input := samples
	if sampleRate != vadSampleRate {
		input = resampleLinear(samples, sampleRate, vadSampleRate)
	}

	det.Reset()
	window := v.cfg.VadWindowSize
	if window <= 0 {
		window = 512
	}
	for i := 0; i < len(input); i += window {
		end := i + window
		if end > len(input) {
			end = len(input)
		}
		det.AcceptWaveform(input[i:end])
	}
	det.Flush()

	for !det.IsEmpty() {
		seg := det.Front()
		det.Pop()
		if seg == nil || len(seg.Samples) == 0 {
			continue
		}
		start := scaleSampleIndex(seg.Start, vadSampleRate, sampleRate)
		end := scaleSampleIndex(seg.Start+len(seg.Samples), vadSampleRate, sampleRate)
		if start < 0 {
			start = 0
		}
		if end > len(samples) {
			end = len(samples)
		}
		if end > start {
			spans = append(spans, speechSpan{Start: start, End: end})
		}
	}
	return spans, true
}

// detectEnergySpans 自适应能量法检测语音段。
//
// 与固定阈值（0.03）不同：阈值由音频自身的底噪分位数与峰值分位数推算，
// 因此既能适应安静的会议室录音，也能适应底噪较大的外录音频，
// 不会因为整体音量偏小就把整段音频判成静音，也不会因为底噪偏高就全是语音。
func detectEnergySpans(samples []float32, sampleRate int, opt vadPostOptions) []speechSpan {
	if len(samples) == 0 || sampleRate <= 0 {
		return nil
	}
	frame := sampleRate * energyFrameMs / 1000
	if frame <= 0 {
		frame = 1
	}

	frames := (len(samples) + frame - 1) / frame
	rms := make([]float64, 0, frames)
	for i := 0; i < frames; i++ {
		start := i * frame
		end := start + frame
		if end > len(samples) {
			end = len(samples)
		}
		var sum float64
		for _, s := range samples[start:end] {
			sum += float64(s) * float64(s)
		}
		rms = append(rms, math.Sqrt(sum/float64(end-start)))
	}

	sorted := append([]float64(nil), rms...)
	sort.Float64s(sorted)
	noise := percentile(sorted, 0.10)
	peak := percentile(sorted, 0.95)

	threshold := math.Max(energyAbsFloor, noise*energyNoiseFactor)
	if peak > 0 && threshold > peak*energyPeakRatio {
		threshold = peak * energyPeakRatio
	}
	if threshold <= 0 {
		threshold = energyAbsFloor
	}

	var spans []speechSpan
	for i, r := range rms {
		if r <= threshold {
			continue
		}
		start := i * frame
		end := start + frame
		if end > len(samples) {
			end = len(samples)
		}
		if len(spans) > 0 && start <= spans[len(spans)-1].End {
			if end > spans[len(spans)-1].End {
				spans[len(spans)-1].End = end
			}
			continue
		}
		spans = append(spans, speechSpan{Start: start, End: end})
	}

	return postProcessSpans(spans, len(samples), sampleRate, opt)
}

// postProcessSpans 归一化语音段：合并相邻的近距语音、补齐首尾音节、丢弃噪声段。
func postProcessSpans(spans []speechSpan, total, sampleRate int, opt vadPostOptions) []speechSpan {
	if len(spans) == 0 || total <= 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })

	maxGap := msToSamplesInt(opt.MinSilenceMs, sampleRate)
	minSpeech := msToSamplesInt(opt.MinSpeechMs, sampleRate)
	pad := msToSamplesInt(opt.PaddingMs, sampleRate)
	if pad < 0 {
		pad = 0
	}

	// 1. 合并静音间隔不超过 maxGap 的相邻段（说话中的自然停顿）。
	merged := make([]speechSpan, 0, len(spans))
	for _, sp := range spans {
		if sp.End <= sp.Start {
			continue
		}
		if len(merged) > 0 && sp.Start-merged[len(merged)-1].End <= maxGap {
			if sp.End > merged[len(merged)-1].End {
				merged[len(merged)-1].End = sp.End
			}
			continue
		}
		merged = append(merged, sp)
	}

	// 2. 首尾补齐后再合并一次（padding 可能让相邻段相接）。
	padded := make([]speechSpan, 0, len(merged))
	for _, sp := range merged {
		start := sp.Start - pad
		end := sp.End + pad
		if start < 0 {
			start = 0
		}
		if end > total {
			end = total
		}
		if end <= start {
			continue
		}
		if len(padded) > 0 && start <= padded[len(padded)-1].End {
			if end > padded[len(padded)-1].End {
				padded[len(padded)-1].End = end
			}
			continue
		}
		padded = append(padded, speechSpan{Start: start, End: end})
	}

	// 3. 丢弃过短的段（键盘声、咳嗽等瞬时噪声）。
	if minSpeech <= 0 {
		return padded
	}
	out := make([]speechSpan, 0, len(padded))
	for _, sp := range padded {
		if sp.End-sp.Start < minSpeech {
			continue
		}
		out = append(out, sp)
	}
	return out
}

// resampleLinear 把音频从 src 采样率线性重采样到 dst。
func resampleLinear(samples []float32, src, dst int) []float32 {
	if len(samples) == 0 || src <= 0 || dst <= 0 {
		return nil
	}
	if src == dst {
		return samples
	}
	n := int(math.Round(float64(len(samples)) * float64(dst) / float64(src)))
	if n <= 0 {
		return nil
	}
	out := make([]float32, n)
	ratio := float64(src) / float64(dst)
	last := len(samples) - 1
	for i := range out {
		pos := float64(i) * ratio
		i0 := int(math.Floor(pos))
		frac := pos - float64(i0)
		if i0 < 0 {
			i0 = 0
		} else if i0 > last {
			i0 = last
		}
		i1 := i0 + 1
		if i1 > last {
			i1 = last
		}
		out[i] = float32(float64(samples[i0])*(1-frac) + float64(samples[i1])*frac)
	}
	return out
}

// scaleSampleIndex 把 srcRate 下的采样下标换算到 dstRate。
func scaleSampleIndex(idx, srcRate, dstRate int) int {
	if srcRate <= 0 || dstRate <= 0 || srcRate == dstRate {
		return idx
	}
	return int(math.Round(float64(idx) * float64(dstRate) / float64(srcRate)))
}

// percentile 取已升序切片的分位值。
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Floor(p * float64(len(sorted)-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// msToSamples 把毫秒换算为采样数。
func msToSamples(ms int64, sampleRate int) int {
	if sampleRate <= 0 {
		return 0
	}
	return int(float64(ms) * float64(sampleRate) / 1000.0)
}

// msToSamplesInt 把毫秒换算为采样数（int 入参版本）。
func msToSamplesInt(ms, sampleRate int) int {
	return msToSamples(int64(ms), sampleRate)
}

// samplesToMs 把采样数换算为毫秒。
func samplesToMs(n, sampleRate int) int {
	if sampleRate <= 0 {
		return 0
	}
	return n * 1000 / sampleRate
}

func (v *vad) warnf(format string, args ...any) {
	if v.logger != nil {
		v.logger.Warning(fmt.Sprintf(format, args...))
	}
}

func (v *vad) infof(format string, args ...any) {
	if v.logger != nil {
		v.logger.Info(fmt.Sprintf(format, args...))
	}
}

func (v *vad) errorf(format string, args ...any) {
	if v.logger != nil {
		v.logger.Error(fmt.Sprintf(format, args...))
	}
}
