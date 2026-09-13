<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { useThemeStore } from '../store/theme';
import { useAuthStore } from '../store/auth';
import ThemeToggle from './ThemeToggle.vue';
import ThemePalette from './ThemePalette.vue';
import UserMenu from './UserMenu.vue';
import {
  TITLE_BAR_HEIGHT,
  syncWindowControlsInsets,
  watchWindowControlsInsets,
  windowApi,
  type WindowState,
} from '../services/windowControls';

const route = useRoute();
const themeStore = useThemeStore();
const auth = useAuthStore();

const platform = ref<NodeJS.Platform>('darwin');
const state = ref<WindowState>({ maximized: false, fullScreen: false, focused: true });
/** 是否使用原生窗口控件覆盖层（Windows / Linux 的 titleBarOverlay） */
const hasOverlayControls = ref(false);

const isMac = computed(() => platform.value === 'darwin');
const isDark = computed(() => themeStore.isDark);
const pageTitle = computed(() => (route.meta.title as string) || 'koi-studio');
/** macOS 全屏时红绿灯自动隐藏，此时无需再为它预留左侧空间 */
const reserveTrafficLights = computed(() => isMac.value && !state.value.fullScreen);

/** 覆盖层配色与自绘标题栏保持一致（浅色取 surface / 深色取暗色 surface） */
const OVERLAY_COLORS = {
  light: { color: '#ffffff', symbolColor: 'rgba(0, 0, 0, 0.88)' },
  dark: { color: '#141414', symbolColor: 'rgba(255, 255, 255, 0.85)' },
};

/** 把主题色同步给原生窗口控件（仅 Windows / Linux 有效） */
function syncOverlayColor() {
  if (!hasOverlayControls.value) return;
  const preset = isDark.value ? OVERLAY_COLORS.dark : OVERLAY_COLORS.light;
  void windowApi.setTitleBarOverlay({ ...preset, height: TITLE_BAR_HEIGHT });
}

let disposeState: (() => void) | undefined;
let disposeInsets: (() => void) | undefined;

onMounted(async () => {
  try {
    platform.value = await windowApi.getPlatform();
    state.value = await windowApi.getState();
    hasOverlayControls.value = platform.value !== 'darwin';
  } catch {
    // 纯浏览器环境（无 Electron IPC）下静默降级：按非 macOS 普通布局渲染
  }
  disposeState = windowApi.onStateChange((next) => {
    state.value = next;
  });
  disposeInsets = watchWindowControlsInsets();
  syncOverlayColor();
});

onBeforeUnmount(() => {
  disposeState?.();
  disposeInsets?.();
});

watch(isDark, () => {
  syncWindowControlsInsets();
  syncOverlayColor();
});
</script>

<template>
  <header
    class="titlebar"
    :class="{
      'is-mac': isMac,
      'is-unfocused': !state.focused,
      'reserve-traffic-lights': reserveTrafficLights,
    }"
  >
    <!-- 左侧：品牌标识 -->
    <div class="titlebar-side titlebar-side--start">
      <span class="brand">
        <span class="brand-mark" aria-hidden="true">
          <svg viewBox="0 0 32 32" width="18" height="18">
            <defs>
              <linearGradient id="koi-titlebar-brand" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0%" stop-color="var(--color-brand)" />
                <stop offset="100%" stop-color="var(--color-accent)" />
              </linearGradient>
            </defs>
            <rect width="32" height="32" rx="9" fill="url(#koi-titlebar-brand)" />
            <path d="M10 23V9l8.4 7L10 23Z" fill="#fff" opacity="0.95" />
            <path d="M19.4 16 25 11.6v8.8L19.4 16Z" fill="#fff" opacity="0.7" />
          </svg>
        </span>
        <span class="brand-name">koi-studio</span>
      </span>
    </div>

    <!-- 中间：当前页面标题（VS Code 标题栏的文件名居中风格） -->
    <div class="titlebar-center" :title="pageTitle">{{ pageTitle }}</div>

    <!-- 右侧：全局操作（原生窗口控件由主进程绘制，这里只需预留安全区） -->
    <div class="titlebar-side titlebar-side--end">
      <ThemePalette class="titlebar-action" />
      <ThemeToggle class="titlebar-action" />
      <!-- 登录用户：仅一枚头像图标，点击弹出账号信息与退出登录 -->
      <UserMenu v-if="auth.isAuthenticated" />
    </div>
  </header>
</template>

<style scoped>
.titlebar {
  /* 原生窗口控件安全区，由 syncWindowControlsInsets() 写入根节点 */
  --titlebar-inline-start: calc(var(--wco-left, 0px) + 10px);
  --titlebar-inline-end: calc(var(--wco-right, 0px) + 10px);

  flex: none;
  display: flex;
  align-items: center;
  height: var(--titlebar-height, 36px);
  padding-inline: var(--titlebar-inline-start) var(--titlebar-inline-end);
  box-sizing: border-box;
  background: var(--color-surface);
  border-bottom: 1px solid var(--color-border);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  /* 整条标题栏即窗口拖拽区，子级交互元素用 no-drag 排除 */
  -webkit-app-region: drag;
  app-region: drag;
  user-select: none;
  -webkit-user-select: none;
}

/* macOS：为原生红绿灯预留左侧空间 */
.titlebar.reserve-traffic-lights {
  padding-inline-start: 76px;
}

.titlebar-side {
  flex: 1 1 0;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

.titlebar-side--end {
  justify-content: flex-end;
}

.titlebar-center {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 46%;
  padding: 0 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--color-text);
  font-weight: var(--font-weight-medium);
  letter-spacing: 0.2px;
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.brand-mark {
  display: inline-flex;
  line-height: 0;
  flex: none;
}

.brand-name {
  color: var(--color-text);
  font-weight: var(--font-weight-semibold);
  letter-spacing: 0.2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 窗口失焦时弱化信息层级（对齐 macOS / VS Code 的静默态） */
.titlebar.is-unfocused .brand-name,
.titlebar.is-unfocused .titlebar-center {
  color: var(--color-text-muted);
}

/* 标题栏内的可交互元素必须排除拖拽，否则无法点击 */
.titlebar :deep(button),
.titlebar :deep(a),
.titlebar :deep(input),
.titlebar :deep(.titlebar-action) {
  -webkit-app-region: no-drag;
  app-region: no-drag;
}
</style>
