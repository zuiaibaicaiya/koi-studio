<script setup lang="ts">
import { reactive, ref, onMounted, watch } from 'vue';
import { useRouter } from 'vue-router';
import { App } from 'antdv-next';
import {
  useMeetingStore,
  type Meeting,
  type UIMeetingStatus,
} from '../../store/meeting';
import { exportMeetingById } from '../../utils/exportMeeting';
import {
  DeleteOutlined,
  SearchOutlined,
  ReloadOutlined,
  DownloadOutlined,
  EyeOutlined,
  PlusOutlined,
} from '@antdv-next/icons';

// 使用 App 上下文的 message / modal 实例，使其继承 ConfigProvider 主题（暗黑模式），
// 避免出现静态方法渲染到 body、不跟随主题变化的问题。
const { message, modal } = App.useApp();

const store = useMeetingStore();

/* ==================== 类型筛选（所有 / 实时会议 / 音频转写） ==================== */
const typeFilter = ref<'all' | 'live' | 'audio'>('all');

/* ==================== 实时会议（对接后端） ==================== */
const meetingColumns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
  { title: '会议名称', dataIndex: 'name', key: 'name', ellipsis: true },
  { title: '参会人员', dataIndex: 'participants', key: 'participants', ellipsis: true },
  { title: '开始时间', dataIndex: 'startTime', key: 'startTime', width: 185 },
  { title: '结束时间', dataIndex: 'endTime', key: 'endTime', width: 185 },
  { title: '音频', dataIndex: 'audioUrl', key: 'audioUrl', width: 280 },
  { title: '状态', dataIndex: 'status', key: 'status', width: 100 },
  { title: '操作', key: 'action', width: 256, fixed: 'right' as const },
];

/* ---- 搜索与分页 ---- */
const searchModel = reactive({});
const meetingKeyword = ref('');
const meetingTimeRange = ref<[string, string] | null>(null);
const meetingStatusFilter = ref<'' | UIMeetingStatus>('');

// 类型 / 状态 以分段选择器展示，操作更直观
const typeOptions = [
  { label: '所有', value: 'all' },
  { label: '实时会议', value: 'live' },
  { label: '音频转写', value: 'audio' },
];
const statusOptions = [
  { label: '全部', value: '' },
  { label: '已预约', value: '已预约' },
  { label: '进行中', value: '进行中' },
  { label: '已结束', value: '已结束' },
];

const meetingPagination = reactive({
  current: 1,
  pageSize: 10,
  showSizeChanger: true,
  showTotal: (t: number) => `共 ${t} 条`,
});

/** 统一加载会议列表：根据类型筛选决定是否按 mode 查询（所有=不区分） */
function loadMeetings() {
  const mode = typeFilter.value === 'all' ? undefined : typeFilter.value;
  store.load({
    mode,
    page: meetingPagination.current,
    pageSize: meetingPagination.pageSize,
    keyword: meetingKeyword.value || undefined,
    status: meetingStatusFilter.value,
    startTime: meetingTimeRange.value?.[0] || undefined,
    endTime: meetingTimeRange.value?.[1] || undefined,
  });
}

function doSearchMeeting() {
  meetingPagination.current = 1;
  loadMeetings();
}

function handleMeetingReset() {
  meetingKeyword.value = '';
  meetingTimeRange.value = null;
  meetingStatusFilter.value = '';
  meetingPagination.current = 1;
  loadMeetings();
}

function onMeetingPageChange(page: number, pageSize: number) {
  meetingPagination.current = page;
  meetingPagination.pageSize = pageSize;
  loadMeetings();
}

watch(meetingKeyword, doSearchMeeting);
watch(meetingTimeRange, doSearchMeeting);
watch(typeFilter, doSearchMeeting);
watch(meetingStatusFilter, doSearchMeeting);

/* ---- 会议操作 ---- */

async function handleMeetingDelete(record: Meeting) {
  try {
    await store.remove(record.id);
    message.success('会议已删除');
  } catch (e: unknown) {
    message.error((e as { message?: string })?.message || '删除失败');
  }
}

async function handleMeetingExportOne(record: Meeting) {
  try {
    const { audioIncluded } = await exportMeetingById(record.id);
    message.success(audioIncluded ? '导出成功' : '已导出会议文本（音频下载失败）');
  } catch (e) {
    message.error('导出失败：' + (e instanceof Error ? e.message : String(e)));
  }
}


function confirmDeleteMeeting(record: Meeting) {
  modal.confirm({
    title: '确认删除该会议？',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: () => handleMeetingDelete(record),
  });
}

/* ---- 跳转会议详情页面 ---- */
const router = useRouter();
function goMeetingDetail(record: Meeting) {
  router.push({ name: 'meetingDetail', params: { id: record.id } });
}

/* ==================== 音频转写相关（与实时会议共用 meetings 表，按 mode=audio 区分） ==================== */

function goCreateTranscription() {
  router.push({ name: 'offlineCreate' });
}

/* ==================== 初始化 ==================== */
onMounted(loadMeetings);
</script>

<template>
  <div class="page">
    <!-- ==================== 工具栏（合并实时会议与音频转写） ==================== -->
    <a-card class="toolbar" variant="borderless">
      <a-form
        class="filter-form"
        layout="horizontal"
        :model="searchModel"
        :label-col="{ span: 6 }"
        :wrapper-col="{ span: 18 }"
        :colon="false"
        @finish="doSearchMeeting"
      >
        <a-row :gutter="[8, 8]">
          <a-col :xs="24" :sm="12" :md="8">
            <a-form-item label="类型">
              <a-select v-model:value="typeFilter" :options="typeOptions" style="width: 100%" />
            </a-form-item>
          </a-col>
          <a-col :xs="24" :sm="12" :md="8">
            <a-form-item label="关键词">
              <a-input v-model:value="meetingKeyword" placeholder="会议名称 / 参会人员" allow-clear style="width: 100%">
                <template #prefix><SearchOutlined /></template>
              </a-input>
            </a-form-item>
          </a-col>
          <a-col :xs="24" :sm="12" :md="8">
            <a-form-item label="状态">
              <a-select
                v-model:value="meetingStatusFilter"
                :options="statusOptions"
                placeholder="全部"
                allow-clear
                style="width: 100%"
              />
            </a-form-item>
          </a-col>
          <a-col :xs="24" :sm="12" :md="8">
            <a-form-item label="时间段" class="time-range-item">
              <a-range-picker
                v-model:value="meetingTimeRange"
                format="YYYY-MM-DD"
                value-format="YYYY-MM-DD"
                :placeholder="['开始日期', '结束日期']"
                style="width: 100%"
              />
            </a-form-item>
          </a-col>
          <a-col :xs="24" :sm="12" :md="8" class="toolbar-actions-col">
            <a-form-item :label="null">
              <a-space class="toolbar-actions">
                <a-button @click="handleMeetingReset">
                  <ReloadOutlined />重置
                </a-button>
                <a-button type="primary" html-type="submit">
                  <SearchOutlined />搜索
                </a-button>
                <a-button type="dashed" @click="goCreateTranscription">
                  <PlusOutlined />新建转写
                </a-button>
              </a-space>
            </a-form-item>
          </a-col>
        </a-row>
      </a-form>
    </a-card>

    <!-- ==================== 数据表格 ==================== -->
    <a-card variant="borderless" class="table-card">
      <a-spin :spinning="store.loading">
        <a-table
          :columns="meetingColumns"
          :data-source="store.list"
          :pagination="{ ...meetingPagination, total: store.total, current: store.page }"
          row-key="id"
          :scroll="{ x: 'max-content' }"
          @change="(pag: any) => pag.current && onMeetingPageChange(pag.current, pag.pageSize)"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'name'">
              <a class="name-link" @click="goMeetingDetail(record)">{{ record.name }}</a>
            </template>
            <template v-else-if="column.key === 'status'">
              <a-tag :color="record.rawStatus === 'ongoing' ? 'green' : record.rawStatus === 'created' ? 'blue' : 'default'">
                <template v-if="record.rawStatus === 'ongoing'">
                  <span class="status-dot dot-active" />
                </template>
                {{ record.status }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'participants'">
              <span>{{ record.participants || '--' }}</span>
            </template>
            <template v-else-if="column.key === 'audioUrl'">
              <audio v-if="record.audioUrl" :src="record.audioUrl" controls preload="metadata" class="audio-player" />
              <span v-else class="muted">--</span>
            </template>
            <template v-else-if="column.key === 'action'">
              <a-space size="small">
                <a-button type="link" size="small" @click="goMeetingDetail(record)">
                  <EyeOutlined />详情
                </a-button>
                <a-button type="link" size="small" @click="handleMeetingExportOne(record)">
                  <DownloadOutlined />导出
                </a-button>
                <a-button type="link" size="small" danger @click="confirmDeleteMeeting(record)">
                  <DeleteOutlined />删除
                </a-button>
              </a-space>
            </template>
          </template>
        </a-table>
      </a-spin>
    </a-card>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* ---- 工具栏 ---- */
.toolbar {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 8px 12px;
}
.toolbar :deep(.ant-form-item-label > label) {
  color: var(--color-text-secondary);
}
.filter-form {
  padding: 8px 0 4px;
}
.filter-form :deep(.ant-form-item) {
  margin-bottom: 0;
}
/* 仅“时间段”项：控件跟随列宽，标签仍与控件同行 */
.filter-form :deep(.time-range-item .ant-picker) {
  width: 100%;
  min-width: 0;
}
.filter-form :deep(.time-range-item .ant-picker-input) {
  min-width: 0;
}
.toolbar-actions-col {
  display: flex;
  align-items: flex-end;
  justify-content: flex-start;
}

/* ---- 表格 ---- */
.table-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
}

/* ---- 状态指示点 ---- */
.status-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  margin-right: 4px;
  vertical-align: middle;
}
.dot-active {
  background: var(--color-success);
  box-shadow: 0 0 6px rgba(82, 196, 26, 0.6);
  animation: pulse 1.5s infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

/* ---- 详情 ---- */
.detail-desc :deep(.ant-descriptions-item-label) {
  font-weight: 500;
  width: 100px;
}
.transcript-text {
  white-space: pre-wrap;
  line-height: 1.8;
  max-height: 300px;
  overflow-y: auto;
  background: var(--color-surface-2);
  padding: 12px;
  border-radius: var(--radius-md);
}
.muted {
  color: var(--color-text-secondary);
}
.name-link {
  color: var(--color-brand);
  cursor: pointer;
  font-weight: 600;
}
.name-link:hover {
  text-decoration: underline;
}
.audio-player {
  width: 240px;
  height: 36px;
}
.audio-player:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 2px;
}
</style>
