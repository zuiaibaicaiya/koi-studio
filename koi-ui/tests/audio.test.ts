import { describe, expect, test } from '@rstest/core';
import { isWavFile, toWavName } from '../src/utils/audio';

describe('toWavName', () => {
  test('替换扩展名为 .wav', () => {
    expect(toWavName('recording.webm')).toBe('recording.wav');
    expect(toWavName('meeting.audio.m4a')).toBe('meeting.audio.wav');
    expect(toWavName('录音.ogg')).toBe('录音.wav');
  });

  test('路径分隔符不算扩展名的一部分', () => {
    expect(toWavName('dir\\name.webm')).toBe('dir\\name.wav');
    expect(toWavName('dir/name.webm')).toBe('dir/name.wav');
  });

  test('无扩展名与空名兜底', () => {
    expect(toWavName('audio')).toBe('audio.wav');
    expect(toWavName('.webm')).toBe('sample.wav');
    expect(toWavName('')).toBe('sample.wav');
  });
});

describe('isWavFile', () => {
  test('按真实 MIME 判断，而不是文件名', () => {
    // MediaRecorder 会把文件名硬编码成 .wav，但内容仍是 webm
    const fakeWavName = new Blob(['x'], { type: 'audio/webm' });
    expect(isWavFile(fakeWavName)).toBe(false);

    const realWav = new Blob(['x'], { type: 'audio/wav' });
    expect(isWavFile(realWav)).toBe(true);
  });

  test('支持 wav 的各种 MIME 别名', () => {
    expect(isWavFile(new Blob([], { type: 'audio/wave' }))).toBe(true);
    expect(isWavFile(new Blob([], { type: 'audio/x-wav' }))).toBe(true);
    expect(isWavFile(new Blob([], { type: 'audio/mpeg' }))).toBe(false);
    expect(isWavFile(new Blob([], { type: '' }))).toBe(false);
  });
});
