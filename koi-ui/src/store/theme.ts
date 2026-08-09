import { defineStore } from 'pinia';
import { computed, ref, watch } from 'vue';

export type ThemeMode = 'light' | 'dark';

/**
 * 主题状态管理：负责明暗模式切换、跟随系统、以及持久化。
 * 视图与 antd 主题均从此 store 读取 `mode` 来同步外观。
 */
export const useThemeStore = defineStore(
  'theme',
  () => {
    const mode = ref<ThemeMode>('light');
    const followSystem = ref(false);

    const isDark = computed(() => mode.value === 'dark');

    function applyToDocument() {
      const root = document.documentElement;
      root.setAttribute('data-theme', mode.value);
      root.classList.toggle('dark', isDark.value);
      root.style.colorScheme = mode.value;
    }

    function setMode(next: ThemeMode) {
      mode.value = next;
      followSystem.value = false;
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

    // 任何模式变化都同步到 DOM
    watch(mode, applyToDocument);

    return { mode, followSystem, isDark, setMode, toggle, setFollowSystem, init };
  },
  {
    persist: true,
  },
);
