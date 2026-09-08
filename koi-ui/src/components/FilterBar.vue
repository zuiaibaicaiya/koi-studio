<script setup lang="ts">
import { ReloadOutlined, SearchOutlined } from '@antdv-next/icons';

/**
 * 统一的筛选栏：上方为页面级操作区（新增 / 导入 / 导出 …），下方为筛选字段 + 查询 / 重置。
 * 所有页面共用同一套栅格节奏，避免各页面搜索区各写一套布局。
 */
withDefaults(
  defineProps<{
    /** 查询按钮文案 */
    searchText?: string;
    /** 重置按钮文案 */
    resetText?: string;
    /** 是否渲染查询 / 重置按钮组 */
    showButtons?: boolean;
  }>(),
  { searchText: '查询', resetText: '重置', showButtons: true },
);

const emit = defineEmits<{ search: []; reset: [] }>();
</script>

<template>
  <a-card class="filter-bar" variant="borderless">
    <!-- 页面级操作区（与筛选区分离，避免和查询 / 重置混在一起） -->
    <div v-if="$slots.title || $slots.actions" class="filter-bar__head">
      <div class="filter-bar__title">
        <slot name="title" />
      </div>
      <div class="filter-bar__actions">
        <slot name="actions" />
      </div>
    </div>

    <!-- 筛选区：回车即可提交查询 -->
    <form class="filter-bar__body" @submit.prevent="emit('search')">
      <slot />
      <div v-if="showButtons" class="filter-bar__buttons">
        <slot name="buttons">
          <a-button type="primary" html-type="submit">
            <template #icon><SearchOutlined /></template>
            {{ searchText }}
          </a-button>
          <a-button @click="emit('reset')">
            <template #icon><ReloadOutlined /></template>
            {{ resetText }}
          </a-button>
        </slot>
      </div>
    </form>
  </a-card>
</template>

<style scoped>
.filter-bar {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
}
.filter-bar :deep(.ant-card-body) {
  padding: 14px 16px;
}

/* ---- 操作区 ---- */
.filter-bar__head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 12px;
  padding-bottom: 12px;
  margin-bottom: 14px;
  border-bottom: 1px dashed var(--color-border);
}
.filter-bar__title {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text);
}
.filter-bar__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-left: auto;
}

/* ---- 筛选区 ---- */
.filter-bar__body {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px 16px;
}
.filter-bar__buttons {
  display: flex;
  align-items: center;
  gap: 8px;
  /* 无论上一行是否排满，按钮始终贴右侧，行满时自动换到下一行 */
  margin-left: auto;
}

/* ---- 小屏：字段与按钮各占整行，按钮等分铺满，触控更友好 ---- */
@media (max-width: 576px) {
  .filter-bar__actions,
  .filter-bar__buttons {
    width: 100%;
    margin-left: 0;
  }
  .filter-bar__buttons :deep(.ant-btn) {
    flex: 1 1 0;
  }
}
</style>
