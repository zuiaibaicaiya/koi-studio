<script setup lang="ts">
import { computed } from 'vue';

defineOptions({ name: 'SystemDashboard' });
import { useRouter } from 'vue-router';
import { useSystemUserStore } from '../../store/systemUser';
import { useHotWordStore } from '../../store/hotWord';
import { useSpeakerStore } from '../../store/speaker';
import BaseBarChart from '../../components/charts/BaseBarChart.vue';
import BaseLineChart from '../../components/charts/BaseLineChart.vue';
import BasePieChart from '../../components/charts/BasePieChart.vue';
import { TeamOutlined, TagsOutlined, SoundOutlined, CheckCircleOutlined } from '@antdv-next/icons';

const router = useRouter();
const userStore = useSystemUserStore();
const hotWordStore = useHotWordStore();
const speakerStore = useSpeakerStore();

const enabledUsers = computed(
  () => userStore.list.filter((u) => u.status === '启用').length,
);

const trend = computed(() => {
  const days = ['周一', '周二', '周三', '周四', '周五', '周六', '周日'];
  let base = 4;
  return days.map((label, i) => {
    base += Math.floor(Math.random() * 4);
    return { label, value: base + i };
  });
});

const roleDist = computed(() => {
  const map: Record<string, number> = {};
  userStore.list.forEach((u) => {
    map[u.role] = (map[u.role] || 0) + 1;
  });
  return Object.entries(map).map(([label, value]) => ({ label, value }));
});

const hotWordByCategory = computed(() => {
  const map: Record<string, number> = {};
  hotWordStore.list.forEach((w) => {
    map[w.category] = (map[w.category] || 0) + 1;
  });
  return Object.entries(map).map(([label, value]) => ({ label, value }));
});

const speakerByLanguage = computed(() => {
  const map: Record<string, number> = {};
  speakerStore.list.forEach((s) => {
    map[s.language] = (map[s.language] || 0) + 1;
  });
  return Object.entries(map).map(([label, value]) => ({ label, value }));
});

const topSpeakers = computed(() =>
  [...speakerStore.list]
    .sort((a, b) => b.sampleCount - a.sampleCount)
    .slice(0, 6)
    .map((s) => ({ label: s.name, value: s.sampleCount })),
);

const cards = computed(() => [
  {
    title: '用户总数',
    value: userStore.list.length,
    icon: TeamOutlined,
    color: '#2f54eb',
    to: 'users',
  },
  {
    title: '热词总数',
    value: hotWordStore.list.length,
    icon: TagsOutlined,
    color: '#faad14',
    to: 'hotWords',
  },
  {
    title: '说话人总数',
    value: speakerStore.list.length,
    icon: SoundOutlined,
    color: '#52c41a',
    to: 'speakers',
  },
  {
    title: '启用用户',
    value: enabledUsers.value,
    icon: CheckCircleOutlined,
    color: '#13c2c2',
    to: 'users',
  },
]);

function go(name: string) {
  router.push({ name });
}
</script>

<template>
  <div class="dashboard">
    <a-row :gutter="16">
      <a-col v-for="c in cards" :key="c.title" :xs="24" :sm="12" :lg="6">
        <a-card class="stat-card" hoverable @click="go(c.to)">
          <div class="stat-card-body">
            <div class="stat-icon" :style="{ background: c.color }">
              <component :is="c.icon" />
            </div>
            <div class="stat-info">
              <div class="stat-title">{{ c.title }}</div>
              <div class="stat-value">{{ c.value }}</div>
            </div>
          </div>
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" class="mt-16">
      <a-col :xs="24" :lg="16">
        <a-card title="近 7 日新增用户趋势" class="chart-card">
          <BaseLineChart :data="trend" color="#2f54eb" :height="240" />
        </a-card>
      </a-col>
      <a-col :xs="24" :lg="8">
        <a-card title="用户角色占比" class="chart-card">
          <BasePieChart :data="roleDist" :height="180" />
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" class="mt-16">
      <a-col :xs="24" :lg="12">
        <a-card title="热词分类分布" class="chart-card">
          <BaseBarChart :data="hotWordByCategory" color="#faad14" :height="240" />
        </a-card>
      </a-col>
      <a-col :xs="24" :lg="12">
        <a-card title="说话人语言分布" class="chart-card">
          <BaseBarChart :data="speakerByLanguage" color="#52c41a" :height="240" />
        </a-card>
      </a-col>
    </a-row>

    <a-row :gutter="16" class="mt-16">
      <a-col :xs="24">
        <a-card title="样本数 Top 6 说话人" class="chart-card">
          <BaseBarChart :data="topSpeakers" color="#13c2c2" :height="220" />
        </a-card>
      </a-col>
    </a-row>
  </div>
</template>

<style scoped>
.mt-16 {
  margin-top: 16px;
}
.stat-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  color: var(--color-text);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
  transition: transform 0.2s ease, box-shadow 0.2s ease, border-color 0.2s ease;
}
.stat-card:hover {
  transform: translateY(-3px);
  box-shadow: var(--shadow-md);
  border-color: var(--color-brand);
}
.stat-card-body {
  display: flex;
  align-items: center;
  gap: 16px;
}
.stat-icon {
  width: 48px;
  height: 48px;
  border-radius: var(--radius-lg);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  color: #fff;
}
.stat-title {
  color: var(--color-text-secondary);
  font-size: 13px;
}
.stat-value {
  font-size: 26px;
  font-weight: 700;
  color: var(--color-text);
}
.chart-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  color: var(--color-text);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-sm);
}
.chart-card :deep(.ant-card-head-title) {
  color: var(--color-text);
}
</style>
