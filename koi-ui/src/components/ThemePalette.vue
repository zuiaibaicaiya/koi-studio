<script setup lang="ts">
import { computed } from 'vue';
import { CheckOutlined } from '@antdv-next/icons';
import { useThemeStore, type ThemeMode } from '../store/theme';
import { THEME_PRESETS, presetMeta } from '../theme/presets';

const themeStore = useThemeStore();

/** 当前色系的元数据（名称 / 预览色带） */
const current = computed(() => presetMeta(themeStore.preset));

/** 明暗三态：跟随系统时用 'system' 表示，避免和 mode 的取值混淆 */
const modeValue = computed(() => (themeStore.followSystem ? 'system' : themeStore.mode));
const modeSummary = computed(() =>
  themeStore.followSystem ? '跟随系统' : themeStore.isDark ? '深色' : '浅色',
);

const modeOptions = [
  { label: '浅色', value: 'light' },
  { label: '深色', value: 'dark' },
  { label: '跟随系统', value: 'system' },
];

function onModeChange(value: string | number) {
  if (value === 'system') {
    themeStore.setFollowSystem(true);
    return;
  }
  themeStore.setMode(value as ThemeMode);
}

function gradient([from, to]: [string, string]) {
  return `linear-gradient(135deg, ${from}, ${to})`;
}

/** 内边距交给 popover 的语义样式，避免为了覆盖 antd 而写全局选择器 */
const popoverStyles = { content: { padding: '14px 14px 12px' } };
</script>

<template>
  <a-popover
    trigger="click"
    placement="bottomRight"
    :arrow="false"
    :styles="popoverStyles"
  >
    <template #content>
      <div class="theme-panel">
        <div class="panel-head">
          <span class="panel-title">外观</span>
          <span class="panel-current">{{ current.label }} · {{ modeSummary }}</span>
        </div>

        <a-segmented
          block
          size="small"
          :options="modeOptions"
          :value="modeValue"
          @change="onModeChange"
        />

        <div class="panel-label">主题色</div>

        <div class="preset-grid" role="radiogroup" aria-label="主题色系">
          <button
            v-for="p in THEME_PRESETS"
            :key="p.key"
            type="button"
            role="radio"
            class="preset"
            :class="{ 'is-active': p.key === themeStore.preset }"
            :aria-checked="p.key === themeStore.preset"
            @click="themeStore.setPreset(p.key)"
          >
            <span class="preset-chip" :style="{ background: gradient(p.swatch) }">
              <CheckOutlined class="preset-check" />
            </span>
            <span class="preset-text">
              <span class="preset-name">{{ p.label }}</span>
              <span class="preset-hint">{{ p.hint }}</span>
            </span>
          </button>
        </div>
      </div>
    </template>

    <button class="palette-trigger" type="button" title="主题风格" aria-label="主题风格">
      <span class="palette-chip" :style="{ background: gradient(current.swatch) }" />
    </button>
  </a-popover>
</template>

<style scoped>
/* ---- 标题栏触发按钮：一枚当前色系的色片，比图标更直观 ---- */
.palette-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  cursor: pointer;
  transition: box-shadow 0.18s ease;
}
.palette-trigger:hover {
  box-shadow: 0 0 0 2px var(--color-brand-soft);
}
.palette-trigger:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 1px;
}
.palette-chip {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.35), var(--shadow-sm);
  transition: transform 0.18s ease;
}
.palette-trigger:hover .palette-chip {
  transform: scale(1.08);
}

/* ---- 面板 ---- */
.theme-panel {
  width: 264px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.panel-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}
.panel-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text);
}
.panel-current {
  font-size: 12px;
  color: var(--color-text-muted);
}
.panel-label {
  font-size: 11px;
  letter-spacing: 0.08em;
  color: var(--color-text-muted);
}

/* ---- 色系网格 ---- */
.preset-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
}
.preset {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  padding: 7px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background: var(--color-surface);
  text-align: left;
  cursor: pointer;
  transition: border-color 0.18s ease, background 0.18s ease;
}
.preset:hover {
  border-color: var(--color-border-strong);
  background: var(--color-surface-2);
}
.preset.is-active {
  border-color: var(--color-brand);
  background: var(--color-brand-soft);
}
.preset:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 1px;
}
.preset-chip {
  flex: none;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  /* 色片的转角跟随意系的形态层，让不同主题的“手感”一致 */
  border-radius: calc(var(--radius-md) - 1px);
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.3);
}
.preset-check {
  font-size: 11px;
  color: #fff;
  opacity: 0;
  transition: opacity 0.18s ease;
}
.preset.is-active .preset-check {
  opacity: 1;
}
.preset-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.preset-name {
  font-size: 12.5px;
  font-weight: 500;
  line-height: 1.35;
  color: var(--color-text);
}
.preset-hint {
  font-size: 11px;
  line-height: 1.3;
  color: var(--color-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
