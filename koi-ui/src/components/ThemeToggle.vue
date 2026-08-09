<script setup lang="ts">
import { computed } from 'vue';
import { useThemeStore } from '../store/theme';
import { MoonOutlined, SunOutlined } from '@antdv-next/icons';

const themeStore = useThemeStore();
const isDark = computed(() => themeStore.isDark);
</script>

<template>
  <button
    class="theme-toggle"
    :title="isDark ? '切换到浅色模式' : '切换到深色模式'"
    :aria-label="isDark ? '切换到浅色模式' : '切换到深色模式'"
    @click="themeStore.toggle()"
  >
    <span class="theme-toggle-track" :class="{ 'is-dark': isDark }">
      <span class="theme-toggle-thumb">
        <component :is="isDark ? MoonOutlined : SunOutlined" />
      </span>
    </span>
  </button>
</template>

<style scoped>
.theme-toggle {
  border: none;
  background: transparent;
  padding: 0;
  cursor: pointer;
  line-height: 0;
}
.theme-toggle-track {
  display: inline-flex;
  align-items: center;
  width: 52px;
  height: 28px;
  border-radius: 999px;
  padding: 3px;
  background: var(--color-brand-soft);
  border: 1px solid var(--color-border);
  transition: background 0.25s ease;
}
.theme-toggle-track.is-dark {
  background: #2a2e3a;
  justify-content: flex-end;
}
.theme-toggle-thumb {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--color-brand);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  box-shadow: var(--shadow-sm);
  transition: transform 0.25s ease, background 0.25s ease;
}
.theme-toggle-track.is-dark .theme-toggle-thumb {
  background: var(--color-accent);
}
</style>
