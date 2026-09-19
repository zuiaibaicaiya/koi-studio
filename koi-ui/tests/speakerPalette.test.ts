import { beforeEach, describe, expect, test } from '@rstest/core';
import { createApp } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useThemeStore } from '../src/store/theme';
import { speakerColorIndex, useSpeakerPalette } from '../src/composables/useSpeakerPalette';

const freshPinia = () => {
  const pinia = createPinia();
  createApp({ render: () => null }).use(pinia);
  setActivePinia(pinia);
  return pinia;
};

describe('speakerColorIndex', () => {
  test('返回 [0, 8) 范围内的整数', () => {
    for (const name of ['张三', '李四', 'Speaker A', '']) {
      const i = speakerColorIndex(name);
      expect(Number.isInteger(i)).toBe(true);
      expect(i).toBeGreaterThanOrEqual(0);
      expect(i).toBeLessThan(8);
    }
  });

  test('同名稳定映射到同一下标', () => {
    expect(speakerColorIndex('张三')).toBe(speakerColorIndex('张三'));
  });

  test('对输入敏感：不同名通常映射不同下标', () => {
    expect(speakerColorIndex('x')).not.toBe(speakerColorIndex('y'));
    expect(speakerColorIndex('说话人1')).not.toBe(speakerColorIndex('说话人2'));
  });
});

describe('useSpeakerPalette', () => {
  beforeEach(() => {
    freshPinia();
  });

  test('默认色系返回 8 个互不相同的有效 hex 色', () => {
    const colors = useSpeakerPalette().value;
    expect(colors).toHaveLength(8);
    for (const c of colors) {
      expect(c).toMatch(/^#[0-9a-f]{6}$/);
    }
    expect(new Set(colors).size).toBe(8);
  });

  test('切换色系后色板随之变化', () => {
    const theme = useThemeStore();
    const before = useSpeakerPalette().value;
    theme.setPreset('amber');
    const after = useSpeakerPalette().value;
    expect(after).toHaveLength(8);
    expect(after).not.toEqual(before);
  });

  test('明暗切换后色板变化（深色用 primaryDark）', () => {
    const theme = useThemeStore();
    const light = useSpeakerPalette().value;
    theme.setMode('dark');
    const dark = useSpeakerPalette().value;
    expect(dark).toHaveLength(8);
    expect(dark).not.toEqual(light);
  });
});
