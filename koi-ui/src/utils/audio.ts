/**
 * 音频工具：把浏览器录制/导入的音频统一转成后端声纹服务可解析的 WAV。
 *
 * MediaRecorder 产出的是 webm/ogg 容器，后端只接受 wav，
 * 因此提交前需在浏览器侧解码并重新编码为 16bit PCM 单声道 WAV。
 */

/** 声纹模型的目标采样率，与后端 speaker.audio.sample_rate 保持一致 */
const TARGET_SAMPLE_RATE = 16000;

type AudioContextCtor = typeof AudioContext;

function getAudioContextCtor(): AudioContextCtor {
  const ctor =
    window.AudioContext ?? (window as unknown as { webkitAudioContext?: AudioContextCtor }).webkitAudioContext;
  if (!ctor) throw new Error('当前浏览器不支持音频解码');
  return ctor;
}

/** 解码任意浏览器可识别的音频容器为 AudioBuffer */
async function decodeAudio(input: Blob): Promise<AudioBuffer> {
  const buffer = await input.arrayBuffer();
  const ctx = new (getAudioContextCtor())();
  try {
    return await ctx.decodeAudioData(buffer);
  } finally {
    void ctx.close();
  }
}

/** 重采样并下混为单声道 */
async function toMono(buffer: AudioBuffer, sampleRate: number): Promise<AudioBuffer> {
  const frames = Math.max(1, Math.ceil(buffer.duration * sampleRate));
  const offline = new OfflineAudioContext(1, frames, sampleRate);
  const source = offline.createBufferSource();
  source.buffer = buffer;
  source.connect(offline.destination);
  source.start();
  return offline.startRendering();
}

/** 把单声道 AudioBuffer 编码为 16bit PCM WAV */
function encodeWav(buffer: AudioBuffer): Blob {
  const samples = buffer.getChannelData(0);
  const dataSize = samples.length * 2;
  const view = new DataView(new ArrayBuffer(44 + dataSize));

  const writeText = (offset: number, text: string) => {
    for (let i = 0; i < text.length; i += 1) view.setUint8(offset + i, text.charCodeAt(i));
  };

  writeText(0, 'RIFF');
  view.setUint32(4, 36 + dataSize, true);
  writeText(8, 'WAVE');
  writeText(12, 'fmt ');
  view.setUint32(16, 16, true); // fmt 块长度
  view.setUint16(20, 1, true); // PCM
  view.setUint16(22, 1, true); // 单声道
  view.setUint32(24, buffer.sampleRate, true);
  view.setUint32(28, buffer.sampleRate * 2, true); // 字节率
  view.setUint16(32, 2, true); // 块对齐
  view.setUint16(34, 16, true); // 位深
  writeText(36, 'data');
  view.setUint32(40, dataSize, true);

  for (let i = 0; i < samples.length; i += 1) {
    const s = Math.max(-1, Math.min(1, samples[i]));
    view.setInt16(44 + i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }

  return new Blob([view.buffer], { type: 'audio/wav' });
}

/** 把文件名的扩展名替换为 .wav */
export function toWavName(name: string): string {
  const base = name.replace(/\.[^./\\]+$/, '');
  return `${base || 'sample'}.wav`;
}

/**
 * 是否已为 wav 内容：以真实 MIME 类型为准。
 * 不能用文件名判断——录音流程会把文件名硬编码成 .wav，但 MediaRecorder
 * 产出的字节仍是 webm，若按文件名放行会跳过转码，导致后端解码报
 * “not a valid wav file”。
 */
export function isWavFile(file: Blob): boolean {
  return file.type === 'audio/wav' || file.type === 'audio/wave' || file.type === 'audio/x-wav';
}

/**
 * 转成后端可用的 WAV 文件。
 * 已是 wav 内容的直接透传（后端自带下混与重采样），其余格式解码后重新编码。
 */
export async function toWavFile(input: Blob, fileName: string, sampleRate = TARGET_SAMPLE_RATE): Promise<File> {
  const name = toWavName(fileName);
  if (isWavFile(input)) {
    return input instanceof File && input.name === name ? input : new File([input], name, { type: 'audio/wav' });
  }
  const decoded = await decodeAudio(input);
  const mono = await toMono(decoded, sampleRate);
  return new File([encodeWav(mono)], name, { type: 'audio/wav' });
}
