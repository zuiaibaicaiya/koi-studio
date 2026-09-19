import { describe, expect, test } from '@rstest/core';
import type { MeetingTranscriptDTO } from '../src/services/meetingApi';
import {
  estimatedWordDurationMs,
  isCJK,
  isCJKChar,
  nominalWordDurationMs,
  normalizeWordSpans,
  parseWordTimestamps,
} from '../src/utils/wordTimestamps';

const makeDTO = (
  partial: Partial<MeetingTranscriptDTO>,
): MeetingTranscriptDTO => ({
  id: 1,
  meeting_id: 1,
  speaker_id: null,
  speaker_name: '',
  text: '',
  start_ms: 0,
  end_ms: 0,
  word_timestamps: [],
  is_final: true,
  ...partial,
});

describe('字符判断与词长估计', () => {
  test('isCJKChar 识别汉字与扩展区', () => {
    expect(isCJKChar('你')).toBe(true);
    expect(isCJKChar('a')).toBe(false);
    expect(isCJKChar('㐀')).toBe(true); // U+3400 扩展 A 区
  });

  test('isCJK 按整词判断', () => {
    expect(isCJK('测试')).toBe(true);
    expect(isCJK('hello')).toBe(false);
    expect(isCJK('测a试')).toBe(true);
  });

  test('nominalWordDurationMs：汉字 300ms、其他 120ms', () => {
    expect(nominalWordDurationMs('你')).toBe(300);
    expect(nominalWordDurationMs('ab')).toBe(240);
    expect(nominalWordDurationMs('你a')).toBe(420);
    expect(nominalWordDurationMs('')).toBe(300);
  });

  test('estimatedWordDurationMs：汉字 500ms、其他 200ms', () => {
    expect(estimatedWordDurationMs('你')).toBe(500);
    expect(estimatedWordDurationMs('ab')).toBe(400);
    expect(estimatedWordDurationMs('')).toBe(500);
  });
});

describe('parseWordTimestamps', () => {
  test('JSON 数组直接解析并归一化', () => {
    const spans = parseWordTimestamps(
      makeDTO({
        start_ms: 0,
        end_ms: 700,
        word_timestamps: [
          { word: '测', start_ms: 0, end_ms: 300 },
          { word: '试', start_ms: 300, end_ms: 700 },
        ],
      }),
    );
    expect(spans).toEqual([
      { word: '测', startMs: 0, endMs: 300 },
      { word: '试', startMs: 300, endMs: 700 },
    ]);
  });

  test('旧版 JSON 字符串格式兼容', () => {
    const spans = parseWordTimestamps(
      makeDTO({
        word_timestamps: JSON.stringify([{ word: '好', start_ms: 100, end_ms: 400 }]) as unknown as MeetingTranscriptDTO['word_timestamps'],
      }),
    );
    expect(spans).toEqual([{ word: '好', startMs: 100, endMs: 400 }]);
  });

  test('非法 JSON 字符串返回空数组', () => {
    expect(
      parseWordTimestamps(makeDTO({ word_timestamps: '{bad json' as unknown as MeetingTranscriptDTO['word_timestamps'] })),
    ).toEqual([]);
  });

  test('非数组 / 空 word_timestamps 返回空数组', () => {
    expect(parseWordTimestamps(makeDTO({}))).toEqual([]);
    expect(
      parseWordTimestamps(makeDTO({ word_timestamps: 'x' as unknown as MeetingTranscriptDTO['word_timestamps'] })),
    ).toEqual([]);
  });
});

describe('normalizeWordSpans：逐字高亮可靠性归一化', () => {
  test('零宽区间向后补足，受词长上界与下一个字起点约束', () => {
    // 早期实时转写历史数据：首字被压成 [0, 0]
    const spans = [
      { word: '测', startMs: 0, endMs: 0 },
      { word: '试', startMs: 320, endMs: 719 },
    ];
    normalizeWordSpans(spans, 719);
    // 「测」按词长最长 500ms，但受下一个字起点 320ms 约束
    expect(spans[0]).toEqual({ word: '测', startMs: 0, endMs: 320 });
  });

  test('末字零宽且身后无空间：向前回退一个常规发音时长', () => {
    const spans = [
      { word: '测', startMs: 0, endMs: 300 },
      { word: '好', startMs: 400, endMs: 400 },
    ];
    normalizeWordSpans(spans, 600);
    // 「好」零宽且是最后一个字（无下一个起点可借），向前回退 500ms，
    // 受前一个字结束点 300ms 约束
    expect(spans[1]).toEqual({ word: '好', startMs: 300, endMs: 400 });
  });

  test('身后没有空间时向前回退一个常规发音时长', () => {
    // 「不」「能」同起点：零宽的「不」向前回退 320ms（300ms 常规时长），
    // 结果与既有回归数据一致：不[20,320] 能[320,719]
    const spans = [
      { word: '不', startMs: 320, endMs: 320 },
      { word: '能', startMs: 320, endMs: 719 },
    ];
    normalizeWordSpans(spans, 719);
    expect(spans[0]).toEqual({ word: '不', startMs: 20, endMs: 320 });
    expect(spans[1]).toEqual({ word: '能', startMs: 320, endMs: 719 });
  });

  test('重叠区间对齐到前一个字结束，避免同时高亮', () => {
    const spans = [
      { word: '测', startMs: 0, endMs: 300 },
      { word: '试', startMs: 100, endMs: 700 },
    ];
    normalizeWordSpans(spans, 700);
    expect(spans[1].startMs).toBe(300);
    expect(spans[1].endMs).toBe(700);
  });

  test('归一化后所有区间长度 > 0 且单调不重叠', () => {
    const spans = [
      { word: '零', startMs: 0, endMs: 0 },
      { word: '宽', startMs: 0, endMs: 0 },
      { word: '重', startMs: 100, endMs: 50 },
      { word: '叠', startMs: 80, endMs: 200 },
    ];
    normalizeWordSpans(spans, 500);
    let prevEnd = -1;
    for (const s of spans) {
      expect(s.endMs).toBeGreaterThan(s.startMs);
      expect(s.startMs).toBeGreaterThanOrEqual(prevEnd);
      prevEnd = s.endMs;
    }
  });

  test('空数组不抛错', () => {
    expect(() => normalizeWordSpans([], 0)).not.toThrow();
  });
});
