<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  data: { label: string; value: number }[];
  height?: number;
}>();

const colors = ['#2f54eb', '#52c41a', '#faad14', '#ff4d4f', '#722ed1', '#13c2c2'];
const R = 70;
const C = 2 * Math.PI * R;
const total = computed(() => props.data.reduce((s, d) => s + d.value, 0) || 1);

const segments = computed(() => {
  let offset = 0;
  return props.data.map((d, i) => {
    const frac = d.value / total.value;
    const len = frac * C;
    const seg = {
      ...d,
      color: colors[i % colors.length],
      dash: len,
      gap: C - len,
      offset: -offset,
    };
    offset += len;
    return seg;
  });
});
</script>

<template>
  <div class="pie-wrap">
    <svg viewBox="0 0 180 180" class="pie" :style="{ height: (height ?? 180) + 'px' }">
      <circle cx="90" cy="90" :r="R" fill="none" stroke="var(--color-border-secondary)" :stroke-width="22" />
      <circle
        v-for="(s, i) in segments"
        :key="i"
        cx="90"
        cy="90"
        :r="R"
        fill="none"
        :stroke="s.color"
        :stroke-width="22"
        :stroke-dasharray="`${s.dash} ${s.gap}`"
        :stroke-dashoffset="s.offset"
        transform="rotate(-90 90 90)"
      />
      <text x="90" y="86" text-anchor="middle" class="total-label">总计</text>
      <text x="90" y="104" text-anchor="middle" class="total-value">{{ total }}</text>
    </svg>
    <ul class="legend">
      <li v-for="(s, i) in segments" :key="i">
        <i :style="{ background: s.color }" />
        <span>{{ s.label }}</span>
        <b>{{ s.value }}</b>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.pie-wrap {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
}
.pie {
  width: auto;
  flex: none;
}
.total-label {
  fill: var(--color-text-muted);
  font-size: 12px;
}
.total-value {
  fill: var(--color-text);
  font-size: 18px;
  font-weight: 600;
}
.legend {
  list-style: none;
  margin: 0;
  padding: 0;
  flex: 1;
  min-width: 140px;
}
.legend li {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 3px 0;
  color: var(--color-text-secondary);
  font-size: 13px;
}
.legend i {
  width: 10px;
  height: 10px;
  border-radius: 2px;
  display: inline-block;
}
.legend span {
  flex: 1;
}
.legend b {
  color: var(--color-text);
}
</style>
