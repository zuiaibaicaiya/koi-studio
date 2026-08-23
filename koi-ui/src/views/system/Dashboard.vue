<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { App } from 'antdv-next';

defineOptions({ name: 'SystemDashboard' });
import {
  TeamOutlined,
  CheckCircleOutlined,
  SoundOutlined,
  TagsOutlined,
  FileTextOutlined,
  VideoCameraOutlined,
  PlayCircleOutlined,
  AudioOutlined,
  ReloadOutlined,
} from '@antdv-next/icons';
import { dashboardApi, type DashboardMeeting, type DashboardStats } from '../../services/dashboardApi';
import BaseBarChart from '../../components/charts/BaseBarChart.vue';
import BaseLineChart from '../../components/charts/BaseLineChart.vue';
import BasePieChart from '../../components/charts/BasePieChart.vue';

const { message } = App.useApp();
const router = useRouter();

const loading = ref(false);
const error = ref('');
const stats = ref<DashboardStats | null>(null);
const updatedAt = ref('');

async function load() {
  loading.value = true;
  error.value = '';
  try {
    stats.value = await dashboardApi.stats();
    updatedAt.value = new Date().toLocaleTimeString('zh-CN', { hour12: false });
  } catch (e) {
    error.value = (e as Error)?.message || '加载失败';
  } finally {
    loading.value = false;
  }
}

onMounted(load);

const overview = computed(() => stats.value?.overview);

const cards = computed(() => [
  { title: '用户总数', value: overview.value?.userTotal ?? 0, icon: TeamOutlined, color: '#2f54eb', to: 'users' },
  { title: '启用用户', value: overview.value?.userActive ?? 0, icon: CheckCircleOutlined, color: '#13c2c2', to: 'users' },
  { title: '说话人总数', value: overview.value?.speakerTotal ?? 0, icon: SoundOutlined, color: '#52c41a', to: 'speakers' },
  { title: '热词总数', value: overview.value?.hotWordTotal ?? 0, icon: TagsOutlined, color: '#faad14', to: 'hotWords' },
  { title: '热词库', value: overview.value?.hotWordLibraryTotal ?? 0, icon: FileTextOutlined, color: '#722ed1', to: 'hotWords' },
  { title: '会议总数', value: overview.value?.meetingTotal ?? 0, icon: VideoCameraOutlined, color: '#2f54eb', to: 'meetings' },
  { title: '进行中会议', value: overview.value?.meetingOngoing ?? 0, icon: PlayCircleOutlined, color: '#eb2f96', to: 'meetings' },
  { title: '转写总数', value: overview.value?.transcriptTotal ?? 0, icon: AudioOutlined, color: '#fa8c16', to: 'meetings' },
]);

const meetingTrend = computed(() =>
  (stats.value?.trends.labels ?? []).map((label, i) => ({
    label,
    value: stats.value?.trends.meetingSeries[i] ?? 0,
  })),
);
const transcriptTrend = computed(() =>
  (stats.value?.trends.labels ?? []).map((label, i) => ({
    label,
    value: stats.value?.trends.transcriptSeries[i] ?? 0,
  })),
);

const userStatusDist = computed(() => stats.value?.userStatusDist ?? []);
const hotWordLibDist = computed(() => stats.value?.hotWordLibDist ?? []);
const topSpeakers = computed(() => stats.value?.topSpeakers ?? []);
const recentMeetings = computed<DashboardMeeting[]>(() => stats.value?.recentMeetings ?? []);

function statusMeta(status: string) {
  switch (status) {
    case 'ongoing':
      return { text: '进行中', color: 'processing' };
    case 'finished':
      return { text: '已结束', color: 'success' };
    default:
      return { text: '已创建', color: 'default' };
  }
}

function modeLabel(mode: string) {
  return mode === 'audio' ? '音频转写' : mode === 'live' ? '实时会议' : mode;
}

const meetingColumns = [
  { title: '会议名称', dataIndex: 'name', key: 'name' },
  { title: '状态', dataIndex: 'status', key: 'status', width: 100 },
  { title: '模式', dataIndex: 'mode', key: 'mode', width: 110 },
  { title: '开始时间', dataIndex: 'startTime', key: 'startTime', width: 180 },
  { title: '转写条数', dataIndex: 'transcriptCount', key: 'transcriptCount', width: 100 },
  { title: '操作', key: 'action', width: 80 },
];

function go(name: string) {
  router.push({ name });
}
function goMeeting(id: number) {
  router.push({ name: 'meetingDetail', params: { id } });
}
function onReload() {
  load().catch(() => message.error('刷新失败'));
}
</script>

<template>
  <div class="dashboard">
    <div class="dashboard-head">
      <div>
        <h3 class="dashboard-title">仪表盘</h3>
        <span v-if="updatedAt" class="dashboard-sub">数据更新于 {{ updatedAt }}</span>
      </div>
      <a-button type="primary" :icon="ReloadOutlined" :loading="loading" @click="onReload">
        刷新
      </a-button>
    </div>

    <a-spin :spinning="loading">
      <a-alert
        v-if="error"
        type="error"
        show-icon
        class="dash-error"
        :message="error"
        description="请确认后端服务已启动并可访问 /api/dashboard/stats"
      >
        <template #action>
          <a-button size="small" danger @click="onReload">重试</a-button>
        </template>
      </a-alert>

      <template v-else>
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
          <a-col :xs="24" :lg="12">
            <a-card title="近 7 日新增会议" class="chart-card">
              <BaseLineChart :data="meetingTrend" color="#2f54eb" :height="240" />
            </a-card>
          </a-col>
          <a-col :xs="24" :lg="12">
            <a-card title="近 7 日新增转写" class="chart-card">
              <BaseLineChart :data="transcriptTrend" color="#fa8c16" :height="240" />
            </a-card>
          </a-col>
        </a-row>

        <a-row :gutter="16" class="mt-16">
          <a-col :xs="24" :lg="8">
            <a-card title="用户状态分布" class="chart-card">
              <BasePieChart :data="userStatusDist" :height="200" />
            </a-card>
          </a-col>
          <a-col :xs="24" :lg="16">
            <a-card title="热词库热词数量分布" class="chart-card">
              <BaseBarChart :data="hotWordLibDist" color="#722ed1" :height="200" />
            </a-card>
          </a-col>
        </a-row>

        <a-row :gutter="16" class="mt-16">
          <a-col :xs="24">
            <a-card title="声纹样本数 Top 6 说话人" class="chart-card">
              <BaseBarChart :data="topSpeakers" color="#13c2c2" :height="220" />
            </a-card>
          </a-col>
        </a-row>

        <a-row :gutter="16" class="mt-16">
          <a-col :xs="24">
            <a-card title="最近会议" class="chart-card">
              <a-table
                :columns="meetingColumns"
                :data-source="recentMeetings"
                :pagination="false"
                size="middle"
                row-key="id"
              >
                <template #bodyCell="{ column, record }">
                  <template v-if="column.key === 'status'">
                    <a-tag :color="statusMeta(record.status).color">
                      {{ statusMeta(record.status).text }}
                    </a-tag>
                  </template>
                  <template v-else-if="column.key === 'mode'">
                    {{ modeLabel(record.mode) }}
                  </template>
                  <template v-else-if="column.key === 'action'">
                    <a @click="goMeeting(record.id)">查看</a>
                  </template>
                </template>
                <template #emptyText>
                  <span class="empty-text">暂无会议数据</span>
                </template>
              </a-table>
            </a-card>
          </a-col>
        </a-row>
      </template>
    </a-spin>
  </div>
</template>

<style scoped>
.mt-16 {
  margin-top: 16px;
}
.dashboard-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.dashboard-title {
  font-size: var(--font-size-xl);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text);
}
.dashboard-sub {
  font-size: var(--font-size-xs);
  color: var(--color-text-muted);
  margin-left: 10px;
}
.dash-error {
  margin-bottom: 16px;
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
  flex: none;
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
.empty-text {
  color: var(--color-text-muted);
}
</style>
