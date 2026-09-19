import { ref } from 'vue';
import { createMicrophoneStream, createSystemAudioStream } from '../services/capture';
import audioProcessorCode from '@/worklets/audio-processor.js?raw';

/** 转写服务要求的采样率 */
const TARGET_SAMPLE_RATE = 16000;
/** 每帧上行的 PCM 采样点数 */
const PCM_CHUNK_SIZE = 512;

export type AudioRecordMode = 'mic' | 'system';

export interface UseAudioCaptureOptions {
  /** 按会议配置返回当前录音方式 */
  getRecordMode: () => AudioRecordMode;
  /** 当前是否应上行音频（如整场暂停时返回 false，仅保留采集链路） */
  shouldSend: () => boolean;
  /** 上行一帧数据：(PCM 分片, flag)，flag=1 音频分片 / 0 结束帧；抛错视为发送失败 */
  sendFrame: (data: ArrayBuffer, flag: number) => void;
  /** 是否允许发送结束帧（如转写连接已建立） */
  canSendFinal: () => boolean;
  /** 采集/上行失败时的提示回调（captureError 之外的 UI 提示） */
  onError?: (message: string) => void;
}

/**
 * 实时转写的音频采集封装：
 * 麦克风 / 系统内录 → AudioWorklet 重采样为 16bit PCM → 固定长度分片上行；
 * 同时维护音量指示（降频计算 RMS）与采集错误状态。
 * 视图层只关心 recording / currentVolume / captureError 与 start / stop / close。
 */
export function useAudioCapture(options: UseAudioCaptureOptions) {
  /** 正在采集音频 */
  const recording = ref(false);
  /** 实时输入音量（0~1），用于状态指示 */
  const currentVolume = ref(0);
  /** 采集 / 权限错误提示 */
  const captureError = ref('');

  let audioContext: AudioContext | null = null;
  let mediaStream: MediaStream | null = null;
  let sourceNode: MediaStreamAudioSourceNode | null = null;
  let workletNode: AudioWorkletNode | null = null;
  /** 零增益节点：保证 worklet 处于渲染图中被驱动，同时避免本机回放造成啸叫 */
  let muteNode: GainNode | null = null;
  /** 系统内录的释放函数（含内部占位视频轨） */
  let stopSystemCapture: (() => void) | null = null;
  let workletReady = false;
  /** 未满一帧的 PCM 余量 */
  let pcmBuffer = new Int16Array(0);
  let volumeTick = 0;

  /** 懒初始化 16kHz AudioContext 并注册 audio-processor 模块 */
  async function ensureAudioGraph() {
    if (!audioContext || audioContext.state === 'closed') {
      audioContext = new AudioContext({ sampleRate: TARGET_SAMPLE_RATE });
      workletReady = false;
    }
    if (audioContext.state === 'suspended') {
      await audioContext.resume();
    }
    if (!workletReady) {
      const blob = new Blob([audioProcessorCode], { type: 'application/javascript' });
      const url = URL.createObjectURL(blob);
      try {
        await audioContext.audioWorklet.addModule(url);
        workletReady = true;
      } finally {
        URL.revokeObjectURL(url);
      }
    }
  }

  /** worklet 回传 16bit PCM：累积成固定长度分片后经回调上行 */
  function handleWorkletMessage(event: MessageEvent) {
    const chunk = new Int16Array(event.data as ArrayBuffer);

    // 音量指示（降频更新，避免高频渲染）
    if (++volumeTick % 4 === 0) {
      let sum = 0;
      for (let i = 0; i < chunk.length; i++) {
        const v = chunk[i] / 32768;
        sum += v * v;
      }
      currentVolume.value = chunk.length ? Math.min(1, Math.sqrt(sum / chunk.length) * 5.5) : 0;
    }

    // 暂停期间不上行音频，仅保留采集链路
    if (!options.shouldSend()) return;

    const merged = new Int16Array(pcmBuffer.length + chunk.length);
    merged.set(pcmBuffer);
    merged.set(chunk, pcmBuffer.length);
    pcmBuffer = merged;

    while (pcmBuffer.length >= PCM_CHUNK_SIZE) {
      const frame = pcmBuffer.slice(0, PCM_CHUNK_SIZE);
      pcmBuffer = pcmBuffer.slice(PCM_CHUNK_SIZE);
      try {
        options.sendFrame(frame.buffer, 1);
      } catch (err) {
        console.error('发送音频数据失败:', err);
        captureError.value = '音频上行失败，请检查转写服务连接';
        void stop(false);
        return;
      }
    }
  }

  /** 按会议配置的录音方式开始采集 */
  async function start() {
    if (recording.value) return;
    captureError.value = '';
    pcmBuffer = new Int16Array(0);

    try {
      await ensureAudioGraph();

      if (options.getRecordMode() === 'mic') {
        mediaStream = await createMicrophoneStream();
      } else {
        const capture = await createSystemAudioStream({ silent: false });
        mediaStream = capture.stream;
        stopSystemCapture = capture.stop;
      }

      sourceNode = audioContext!.createMediaStreamSource(mediaStream);
      workletNode = new AudioWorkletNode(audioContext!, 'audio-processor');
      workletNode.port.onmessage = handleWorkletMessage;
      muteNode = audioContext!.createGain();
      muteNode.gain.value = 0;

      sourceNode.connect(workletNode);
      workletNode.connect(muteNode);
      muteNode.connect(audioContext!.destination);

      // 用户在系统层结束共享 / 拔出设备时同步收尾
      const track = mediaStream.getAudioTracks()[0];
      if (track) track.onended = () => void stop();

      recording.value = true;
    } catch (err) {
      captureError.value = (err as Error)?.message || '音频采集启动失败';
      options.onError?.(captureError.value);
      await stop(false);
    }
  }

  /**
   * 结束采集并释放音频链路。
   * @param sendFinal 是否向后端发送结束帧（flag=0），用于触发最后一段文本定稿
   */
  async function stop(sendFinal = true) {
    const wasRecording = recording.value;
    recording.value = false;

    if (mediaStream) {
      mediaStream.getTracks().forEach((t) => t.stop());
      mediaStream = null;
    }
    if (stopSystemCapture) {
      stopSystemCapture();
      stopSystemCapture = null;
    }
    if (workletNode) {
      workletNode.port.onmessage = null;
      workletNode.disconnect();
      workletNode = null;
    }
    if (sourceNode) {
      sourceNode.disconnect();
      sourceNode = null;
    }
    if (muteNode) {
      muteNode.disconnect();
      muteNode = null;
    }

    pcmBuffer = new Int16Array(0);
    currentVolume.value = 0;

    if (wasRecording && sendFinal && options.canSendFinal()) {
      options.sendFrame(new ArrayBuffer(0), 0);
      // 等待结束帧发出，便于后端定稿最后一段文本
      await new Promise((resolve) => setTimeout(resolve, 300));
    }
  }

  /** 恢复上行前清掉暂停期间的残留音频，并唤醒上下文 */
  function prepareResume() {
    pcmBuffer = new Int16Array(0);
    void audioContext?.resume();
  }

  /** 释放 AudioContext（仅在离开页面时调用） */
  function close() {
    if (audioContext && audioContext.state !== 'closed') {
      void audioContext.close();
    }
    audioContext = null;
    workletReady = false;
  }

  return {
    recording,
    currentVolume,
    captureError,
    start,
    stop,
    prepareResume,
    close,
  };
}
