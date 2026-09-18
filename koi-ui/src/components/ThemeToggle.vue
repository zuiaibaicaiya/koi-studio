<script setup lang="ts">
import { computed } from 'vue';
import { useThemeStore } from '../store/theme';
import { MoonOutlined, SunOutlined } from '@antdv-next/icons';

const themeStore = useThemeStore();
const isDark = computed(() => themeStore.isDark);
const label = computed(() => (isDark.value ? '切换到浅色模式' : '切换到深色模式'));
</script>

<template>
  <button class="theme-toggle" type="button" :title="label" :aria-label="label" @click="themeStore.toggle()">
    <span class="theme-toggle-track" :class="{ 'is-dark': isDark }">
      <span class="theme-toggle-thumb">
        <component :is="isDark ? MoonOutlined : SunOutlined" class="theme-toggle-icon" />
      </span>
    </span>
  </button>
</template>

<style scoped>
/* 点击热区对齐标题栏其他图标按钮（30px），胶囊本身更收敛，避免抢视觉重心 */
.theme-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: auto;
  height: 30px;
  padding: 0;
  border: none;
  border-radius: 999px;
  background: transparent;
  cursor: pointer;
  line-height: 0;
}
.theme-toggle:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 1px;
}
.theme-toggle-track {
  box-sizing: border-box;
  display: inline-flex;
  align-items: center;
  width: 42px;
  height: 22px;
  padding: 2px;
  border-radius: 999px;
  background: var(--color-brand-soft);
  border: 1px solid var(--color-border);
  transition: background 0.25s ease, border-color 0.25s ease;
}
.theme-toggle-track.is-dark {
  /* 深色下轨道取中性次级边框色，避免写死与主题脱节的深蓝灰 */
  background: var(--color-border-secondary);
  justify-content: flex-end;
}
.theme-toggle-thumb {
  flex: none;
  box-sizing: border-box;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--color-brand);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: var(--shadow-sm);
  transition: background 0.25s ease;
}
.theme-toggle-track.is-dark .theme-toggle-thumb {
  background: var(--color-accent);
}
/* 显式钉住图标字号：anticon 的 svg 宽度是 1em，防止继承到标题栏字号后放大 */
.theme-toggle-icon {
  font-size: 10px;
  line-height: 1;
}
</style>
