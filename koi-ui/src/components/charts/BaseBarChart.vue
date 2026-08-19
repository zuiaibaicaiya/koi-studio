<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  data: { label: string; value: number }[];
  color?: string;
  height?: number;
}>();

const W = 480;
const H = computed(() => props.height ?? 220);
const padL = 40;
const padR = 12;
const padT = 12;
const padB = 30;
const innerW = W - padL - padR;
const innerH = computed(() => H.value - padB - padT);
const max = computed(() =>
  Math.max(1, ...props.data.map((d) => Number(d.value) || 0)),
);
const slotW = computed(() => (props.data.length ? innerW / props.data.length : 0));
const barW = computed(() => Math.min(48, (slotW.value || 0) * 0.6));

const bars = computed(() =>
  props.data.map((d, i) => {
    const h = (d.value / max.value) * innerH.value;
    const x = padL + i * slotW.value + (slotW.value - barW.value) / 2;
    const y = padT + innerH.value - h;
    return { ...d, x, y, h };
  }),
);

const gridLines = computed(() => {
  const n = 4;
  return Array.from({ length: n + 1 }, (_, i) => {
    const val = (max.value / n) * i;
    const y = padT + innerH.value - (val / max.value) * innerH.value;
    return { y, val: Math.round(val) };
  });
});
</script>

<template>
  <svg :viewBox="`0 0 ${W} ${H}`" class="chart" preserveAspectRatio="xMidYMid meet">
    <line
      v-for="(g, i) in gridLines"
      :key="'g' + i"
      :x1="padL"
      :x2="W - padR"
      :y1="g.y"
      :y2="g.y"
      stroke="rgba(255,255,255,0.12)"
    />
    <text
      v-for="(g, i) in gridLines"
      :key="'t' + i"
      :x="padL - 6"
      :y="g.y + 4"
      text-anchor="end"
      class="axis"
    >
      {{ g.val }}
    </text>
    <rect
      v-for="(b, i) in bars"
      :key="'b' + i"
      :x="b.x"
      :y="b.y"
      :width="barW"
      :height="b.h"
      :fill="color || '#2f54eb'"
      rx="3"
    />
    <text
      v-for="(b, i) in bars"
      :key="'v' + i"
      :x="b.x + barW / 2"
      :y="b.y - 5"
      text-anchor="middle"
      class="val"
    >
      {{ b.value }}
    </text>
    <text
      v-for="(b, i) in bars"
      :key="'l' + i"
      :x="b.x + barW / 2"
      :y="H - padB + 16"
      text-anchor="middle"
      class="axis"
    >
      {{ b.label }}
    </text>
  </svg>
</template>

<style scoped>
.chart {
  width: 100%;
  height: auto;
}
.axis {
  fill: rgba(255, 255, 255, 0.6);
  font-size: 11px;
}
.val {
  fill: rgba(255, 255, 255, 0.85);
  font-size: 10px;
}
</style>
