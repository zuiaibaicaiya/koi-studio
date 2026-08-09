// AudioWorklet processor: 统一采用 audio-processor 方式采集 PCM，无兼容/降级分支
class AudioProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.bufferSize = 512;
    this.buffer = new Float32Array(this.bufferSize);
    this.bufferIndex = 0;
  }

  process(inputs) {
    const channel = inputs[0][0];

    for (let i = 0; i < channel.length; i++) {
      this.buffer[this.bufferIndex] = channel[i];
      this.bufferIndex++;

      // 缓冲区满则输出 16bit PCM 到主线程
      if (this.bufferIndex >= this.bufferSize) {
        this.port.postMessage(this.toPCM16(this.buffer));
        this.bufferIndex = 0;
      }
    }

    return true;
  }

  toPCM16(float32) {
    const buffer = new ArrayBuffer(float32.length * 2);
    const view = new DataView(buffer);

    for (let i = 0; i < float32.length; i++) {
      const sample = Math.max(-1, Math.min(1, float32[i]));
      const int16 = sample < 0 ? sample * 0x8000 : sample * 0x7fff;
      view.setInt16(i * 2, int16, true);
    }

    return buffer;
  }
}

registerProcessor('audio-processor', AudioProcessor);
