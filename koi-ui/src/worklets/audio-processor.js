// AudioWorklet processor for capturing PCM audio data
class AudioProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.bufferSize = 512; // 512 frames per chunk
    this.buffer = new Float32Array(this.bufferSize);
    this.bufferIndex = 0;
  }

  process(inputs, outputs, params) {
    // Get the first input channel
    const input = inputs[0];
    if (!input || input.length === 0) {
      return true;
    }

    const channel = input[0]; // Mono channel

    // Process each sample
    for (let i = 0; i < channel.length; i++) {
      // Store the sample in the buffer
      this.buffer[this.bufferIndex] = channel[i];
      this.bufferIndex++;

      // If the buffer is full, send it to the main thread
      if (this.bufferIndex >= this.bufferSize) {
        // Convert Float32 to 16-bit PCM
        const pcmBuffer = this.float32To16BitPCM(this.buffer);
        this.port.postMessage(pcmBuffer);
        
        // Reset buffer index
        this.bufferIndex = 0;
      }
    }

    return true;
  }

  // Convert Float32 audio data to 16-bit PCM
  float32To16BitPCM(float32Array) {
    const buffer = new ArrayBuffer(float32Array.length * 2); // 2 bytes per sample
    const view = new DataView(buffer);
    
    for (let i = 0; i < float32Array.length; i++) {
      const sample = Math.max(-1, Math.min(1, float32Array[i]));
      const int16 = sample < 0 ? sample * 0x8000 : sample * 0x7FFF;
      view.setInt16(i * 2, int16, true); // Little-endian
    }
    
    return buffer;
  }
}

// Register the processor
registerProcessor('audio-processor', AudioProcessor);
