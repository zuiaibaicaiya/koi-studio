import { defineStore } from 'pinia';
import { computed, ref, watch } from 'vue';
import { DEFAULT_PRESET, presetMeta, type ThemePreset } from '../theme/presets';

export type { ThemePreset };

export type ThemeMode = 'light' | 'dark';

/**
 * 主题状态：外观由两个正交维度组合而成 ——
 *  - mode   （light / dark / 跟随系统）：决定中性层，即表面、边框、文本、阴影；
 *  - preset （六个色系）：决定品牌层与形态层，即主色、浅底、侧栏深底与圆角尺度。
 * 两者都会写到 <html> 上（data-theme / data-preset），供 index.css 与 antd 同时消费。
 */
export const useThemeStore = defineStore(
  'theme',
  () => {
    const mode = ref<ThemeMode>('light');
    const preset = ref<ThemePreset>(DEFAULT_PRESET);
    const followSystem = ref(false);

    const isDark = computed(() => mode.value === 'dark');

    /** 形态层：把色系的圆角尺度写进 CSS 变量，供自定义组件与 antd 之外的样式复用 */
    function applyShape() {
      const { radius } = presetMeta(preset.value);
      const root = document.documentElement;
      root.style.setProperty('--radius-sm', `${radius.sm}px`);
      root.style.setProperty('--radius-md', `${radius.md}px`);
      root.style.setProperty('--radius-lg', `${radius.lg}px`);
      root.style.setProperty('--radius-xl', `${radius.xl}px`);
    }

    function applyToDocument() {
      const root = document.documentElement;
      root.setAttribute('data-theme', mode.value);
      root.setAttribute('data-preset', preset.value);
      root.classList.toggle('dark', isDark.value);
      root.style.colorScheme = mode.value;
      applyShape();
    }

    function setMode(next: ThemeMode) {
      mode.value = next;
      followSystem.value = false;
      applyToDocument();
    }

    function setPreset(next: ThemePreset) {
      preset.value = next;
      applyToDocument();
    }

    function toggle() {
      setMode(isDark.value ? 'light' : 'dark');
    }

    function setFollowSystem(on: boolean) {
      followSystem.value = on;
      if (on) {
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        mode.value = prefersDark ? 'dark' : 'light';
        applyToDocument();
      }
      // 关闭跟随时保留当前明暗，避免界面突然跳变
    }

    /** 应用启动时的初始化：优先读取持久化值，再决定是否跟随系统。 */
    function init() {
      applyToDocument();
      if (followSystem.value && typeof window !== 'undefined') {
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        mode.value = prefersDark ? 'dark' : 'light';
        applyToDocument();
      }

      // 跟随系统变化时自动切换（仅在开启跟随模式时生效）
      if (typeof window !== 'undefined') {
        window
          .matchMedia('(prefers-color-scheme: dark)')
          .addEventListener('change', (e) => {
            if (followSystem.value) {
              mode.value = e.matches ? 'dark' : 'light';
              applyToDocument();
            }
          });
      }
    }

    // 任何维度变化都同步到 DOM
    watch(mode, applyToDocument);
    watch(preset, applyToDocument);

    return {
      mode,
      preset,
      followSystem,
      isDark,
      setMode,
      setPreset,
      toggle,
      setFollowSystem,
      init,
    };
  },
  {
    persist: true,
  },
);
