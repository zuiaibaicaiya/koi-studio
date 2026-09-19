import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { beforeEach, describe, expect, test } from '@rstest/core';
import { createPinia, setActivePinia } from 'pinia';
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate';
import { createApp, nextTick } from 'vue';
import { DEFAULT_PRESET, presetMeta, THEME_PRESETS } from '../src/theme/presets';
import { useThemeStore } from '../src/store/theme';

/**
 * 创建一个「已安装」的 pinia：pinia.use() 在 app.use(pinia) 之前只会把插件
 * 放进待安装队列（_p 为空），测试环境没有真实 Vue app，必须手动 install
 * 才能让持久化等插件在 store 创建时生效。
 */
const freshPinia = () => {
  const pinia = createPinia();
  pinia.use(piniaPluginPersistedstate);
  const app = createApp({ render: () => null });
  app.use(pinia);
  setActivePinia(pinia);
  return pinia;
};

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute('data-theme');
  document.documentElement.removeAttribute('data-preset');
  freshPinia();
});

describe('theme/presets 元数据', () => {
  test('六个色系 key 完整且默认为靛青', () => {
    expect(THEME_PRESETS.map((p) => p.key)).toEqual([
      'indigo',
      'teal',
      'celadon',
      'plum',
      'amber',
      'graphite',
    ]);
    expect(DEFAULT_PRESET).toBe('indigo');
  });

  test('presetMeta 未知 key 回退默认色系', () => {
    expect(presetMeta('not-exist' as never).key).toBe('indigo');
    expect(presetMeta('amber').radius).toEqual({ sm: 5, md: 8, lg: 10, xl: 14 });
  });

  test('preset key 与 index.css 的 [data-preset] 段落一一对应', () => {
    const css = readFileSync(join(process.cwd(), 'src/index.css'), 'utf8');
    for (const key of THEME_PRESETS.map((p) => p.key)) {
      expect(css).toContain(`[data-preset='${key}']`);
    }
    // 明暗两套中性层都必须存在
    expect(css).toContain("[data-theme='light']");
    expect(css).toContain("[data-theme='dark']");
  });
});

describe('useThemeStore', () => {
  test('init / setMode 同步 data-theme 与 dark class', () => {
    const store = useThemeStore();
    // 与 App.vue 启动流程一致：先 init 应用持久化/当前值到 DOM
    store.init();
    expect(store.mode).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');

    store.setMode('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(store.isDark).toBe(true);

    store.toggle();
    expect(store.mode).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  test('setPreset 同步 data-preset 与圆角 CSS 变量', () => {
    const store = useThemeStore();
    store.setPreset('amber');
    expect(document.documentElement.getAttribute('data-preset')).toBe('amber');
    const style = document.documentElement.style;
    expect(style.getPropertyValue('--radius-sm')).toBe('5px');
    expect(style.getPropertyValue('--radius-md')).toBe('8px');
    expect(style.getPropertyValue('--radius-lg')).toBe('10px');
    expect(style.getPropertyValue('--radius-xl')).toBe('14px');
  });

  test('setFollowSystem 开启后按系统偏好切换（happy-dom 默认浅色）', () => {
    const store = useThemeStore();
    store.setMode('dark');
    store.setFollowSystem(true);
    expect(store.mode).toBe('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');

    // 关闭跟随时保留当前明暗
    store.setFollowSystem(false);
    expect(store.mode).toBe('light');
  });

  test('主题选择持久化到 localStorage 并可在新 store 中恢复', async () => {
    const store = useThemeStore();
    store.setMode('dark');
    store.setPreset('teal');
    // 持久化订阅的 flush 是异步的，需等 microtask 排空
    await nextTick();
    expect(localStorage.getItem('theme')).toBeTruthy();

    freshPinia();
    const restored = useThemeStore();
    expect(restored.mode).toBe('dark');
    expect(restored.preset).toBe('teal');
  });
});
