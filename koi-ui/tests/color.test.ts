import { describe, expect, test } from '@rstest/core';
import { hexToHsl, hslToHex, withAlpha } from '../src/utils/color';

describe('hexToHsl', () => {
  test('标准六位 hex', () => {
    expect(hexToHsl('#ff0000')).toEqual({ h: 0, s: 1, l: 0.5 });
    expect(hexToHsl('#00ff00')).toEqual({ h: 120, s: 1, l: 0.5 });
    expect(hexToHsl('#0000ff')).toEqual({ h: 240, s: 1, l: 0.5 });
  });

  test('无 # 前缀与三位缩写', () => {
    expect(hexToHsl('ff0000')).toEqual({ h: 0, s: 1, l: 0.5 });
    expect(hexToHsl('#f00')).toEqual({ h: 0, s: 1, l: 0.5 });
  });

  test('灰阶无色相', () => {
    const gray = hexToHsl('#808080')!;
    expect(gray.h).toBe(0);
    expect(gray.s).toBe(0);
    expect(Math.abs(gray.l - 0.502)).toBeLessThan(0.001);
    expect(hexToHsl('#000000')).toEqual({ h: 0, s: 0, l: 0 });
  });

  test('非法输入返回 null', () => {
    expect(hexToHsl('not-a-color')).toBeNull();
    expect(hexToHsl('#12345')).toBeNull();
    expect(hexToHsl('')).toBeNull();
  });
});

describe('hslToHex / hexToHsl 往返', () => {
  test('纯色映射正确', () => {
    expect(hslToHex(0, 1, 0.5)).toBe('#ff0000');
    expect(hslToHex(120, 1, 0.5)).toBe('#00ff00');
    expect(hslToHex(240, 1, 0.5)).toBe('#0000ff');
  });

  test('色相取模与负数归一化', () => {
    expect(hslToHex(-90, 1, 0.5)).toBe(hslToHex(270, 1, 0.5));
    expect(hslToHex(450, 1, 0.5)).toBe(hslToHex(90, 1, 0.5));
  });

  test('常见品牌色往返误差不超过 ±1/255', () => {
    const cases = ['#2f54eb', '#0e7490', '#2e7d63', '#722ed1', '#b3760f', '#3c4351'];
    for (const hex of cases) {
      const { h, s, l } = hexToHsl(hex)!;
      const back = hslToHex(h, s, l);
      for (let i = 0; i < 3; i++) {
        const a = parseInt(hex.slice(1 + i * 2, 3 + i * 2), 16);
        const b = parseInt(back.slice(1 + i * 2, 3 + i * 2), 16);
        expect(Math.abs(a - b)).toBeLessThanOrEqual(1);
      }
    }
  });
});

describe('withAlpha', () => {
  test('hex → rgba', () => {
    expect(withAlpha('#ff0000', 0.5)).toBe('rgba(255, 0, 0, 0.5)');
    expect(withAlpha('2f54eb', 0)).toBe('rgba(47, 84, 235, 0)');
    expect(withAlpha('#abc', 1)).toBe('rgba(170, 187, 204, 1)');
  });

  test('非 hex 原样返回，避免生成非法颜色', () => {
    expect(withAlpha('rgb(1, 2, 3)', 0.5)).toBe('rgb(1, 2, 3)');
    expect(withAlpha('red', 0.5)).toBe('red');
    expect(withAlpha('', 0.5)).toBe('');
  });
});
