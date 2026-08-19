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

const points = computed(() =>
  props.data.map((d, i) => {
    const x =
      padL +
      (props.data.length <= 1 ? innerW / 2 : (i / (props.data.length - 1)) * innerW);
    const y = padT + innerH.value - (d.value / max.value) * innerH.value;
    return { x, y, ...d };
  }),
);

const linePath = computed(() =>
  points.value.map((p, i) => (i ? 'L' : 'M') + p.x + ' ' + p.y).join(' '),
);
const areaPath = computed(() => {
  const first = points.value[0];
  const last = points.value[points.value.length - 1];
  if (!first || !last) return '';
  return `${linePath.value} L ${last.x} ${padT + innerH.value} L ${first.x} ${
    padT + innerH.value
  } Z`;
});

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
    <path :d="areaPath" :fill="color || '#2f54eb'" fill-opacity="0.15" />
    <path
      :d="linePath"
      fill="none"
      :stroke="color || '#2f54eb'"
      stroke-width="2"
      stroke-linejoin="round"
      stroke-linecap="round"
    />
    <circle
      v-for="(p, i) in points"
      :key="'p' + i"
      :cx="p.x"
      :cy="p.y"
      r="3"
      :fill="color || '#2f54eb'"
    />
    <text
      v-for="(p, i) in points"
      :key="'v' + i"
      :x="p.x"
      :y="p.y - 8"
      text-anchor="middle"
      class="val"
    >
      {{ p.value }}
    </text>
    <text
      v-for="(p, i) in points"
      :key="'l' + i"
      :x="p.x"
      :y="H - padB + 16"
      text-anchor="middle"
      class="axis"
    >
      {{ p.label }}
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
