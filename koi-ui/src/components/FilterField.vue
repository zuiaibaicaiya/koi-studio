<script setup lang="ts">
/**
 * 筛选字段：标签 + 控件，宽度自适应，与 FilterBar 配合使用。
 */
withDefaults(defineProps<{ label?: string; /** 控件基准宽度（px） */ width?: number }>(), {
  label: '',
  width: 200,
});
</script>

<template>
  <div class="filter-field" :style="{ '--ff-basis': `${width}px` }">
    <span v-if="label" class="filter-field__label">{{ label }}</span>
    <div class="filter-field__control">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.filter-field {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1 1 var(--ff-basis);
  min-width: min(100%, var(--ff-basis));
  max-width: calc(var(--ff-basis) + 96px);
}
.filter-field__label {
  flex: none;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  white-space: nowrap;
}
.filter-field__control {
  flex: 1 1 auto;
  min-width: 0;
}
/* 控件跟随字段宽度，页面内无需再写死 width */
.filter-field__control > :deep(.ant-input),
.filter-field__control > :deep(.ant-input-affix-wrapper),
.filter-field__control > :deep(.ant-input-number),
.filter-field__control > :deep(.ant-select),
.filter-field__control > :deep(.ant-picker),
.filter-field__control > :deep(.ant-cascader) {
  width: 100%;
}

@media (max-width: 576px) {
  .filter-field {
    max-width: 100%;
  }
}
</style>
