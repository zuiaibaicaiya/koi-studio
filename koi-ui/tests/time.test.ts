import { describe, expect, test } from '@rstest/core';
import { formatClock, formatOffset, formatPreciseMs, formatTimestamp } from '../src/utils/time';

describe('formatOffset：毫秒偏移 → MM:SS / HH:MM:SS', () => {
  test('一分钟以内补零显示', () => {
    expect(formatOffset(0)).toBe('00:00');
    expect(formatOffset(999)).toBe('00:00');
    expect(formatOffset(1000)).toBe('00:01');
    expect(formatOffset(59_999)).toBe('00:59');
  });

  test('一小时以内显示 MM:SS', () => {
    expect(formatOffset(61_000)).toBe('01:01');
    expect(formatOffset(3_599_000)).toBe('59:59');
  });

  test('超过一小时显示 HH:MM:SS', () => {
    expect(formatOffset(3_600_000)).toBe('01:00:00');
    expect(formatOffset(3_661_500)).toBe('01:01:01');
  });

  test('非法输入兜底为 00:00', () => {
    expect(formatOffset(-5)).toBe('00:00');
    expect(formatOffset(Number.NaN)).toBe('00:00');
  });
});

describe('formatTimestamp：毫秒时间戳 → HH:MM:SS（支持天）', () => {
  test('一天以内不显示天', () => {
    expect(formatTimestamp(0)).toBe('00:00:00');
    expect(formatTimestamp(3_723_456)).toBe('01:02:03');
  });

  test('超过一天带「N天」前缀', () => {
    expect(formatTimestamp(86_400_000)).toBe('1天 00:00:00');
    expect(formatTimestamp(86_400_000 + 3_600_000)).toBe('1天 01:00:00');
  });
});

describe('formatClock：秒 → 播放器 MM:SS', () => {
  test('基本格式化', () => {
    expect(formatClock(0)).toBe('00:00');
    expect(formatClock(65.4)).toBe('01:05');
    expect(formatClock(600)).toBe('10:00');
  });

  test('非法输入兜底为 00:00', () => {
    expect(formatClock(-1)).toBe('00:00');
    expect(formatClock(Number.NaN)).toBe('00:00');
  });
});

describe('formatPreciseMs：毫秒级 MM:SS.mmm', () => {
  test('毫秒部分固定三位补零', () => {
    expect(formatPreciseMs(0)).toBe('00:00.000');
    expect(formatPreciseMs(1_234)).toBe('00:01.234');
    expect(formatPreciseMs(61_020)).toBe('01:01.020');
  });

  test('非法输入兜底为 00:00.000', () => {
    expect(formatPreciseMs(-100)).toBe('00:00.000');
    expect(formatPreciseMs(Number.NaN)).toBe('00:00.000');
  });
});
