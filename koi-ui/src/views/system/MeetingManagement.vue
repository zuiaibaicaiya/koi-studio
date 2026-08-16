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
import { meetingApi } from '../../services/meetingApi';
import {
  EditOutlined,
  DeleteOutlined,
  SearchOutlined,
  ReloadOutlined,
  DownloadOutlined,
  EyeOutlined,
  PlayCircleOutlined,
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
  { title: '操作', key: 'action', width: 200, fixed: 'right' as const },
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

/* ---- 会议弹窗 ---- */
const meetingModalVisible = ref(false);
const meetingEditingId = ref<number | null>(null);
const meetingSubmitting = ref(false);
const meetingForm = reactive({
  name: '',
  participants: '',
  speakerIds: '',
  hotWordLibraryIds: '',
  startTime: '',
  endTime: '',
  status: '已预约' as UIMeetingStatus,
});

function openMeetingEdit(record: Meeting) {
  meetingEditingId.value = record.id;
  Object.assign(meetingForm, {
    name: record.name,
    participants: record.participants,
    speakerIds: record.speakerIds,
    hotWordLibraryIds: record.hotWordLibraryIds,
    startTime: record.startTime,
    endTime: record.endTime,
    status: record.status,
  });
  meetingModalVisible.value = true;
}

async function handleMeetingSubmit() {
  if (!meetingForm.name.trim()) { message.warning('请输入会议名称'); return; }
  if (!meetingForm.startTime.trim() || !meetingForm.endTime.trim()) { message.warning('请选择开始和结束时间'); return; }
  if (!meetingEditingId.value) return;
  meetingSubmitting.value = true;
  try {
    await store.update(meetingEditingId.value, { ...meetingForm });
    message.success('会议更新成功');
    meetingPagination.current = store.page;
  } catch (e: unknown) {
    message.error((e as { message?: string })?.message || '操作失败');
  } finally {
    meetingSubmitting.value = false;
    meetingModalVisible.value = false;
  }
}

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

/* ---- 查看转写文本弹窗（拉取真实转写记录） ---- */
const transcriptDetailVisible = ref(false);
const transcriptDetailLoading = ref(false);
const transcriptDetailTitle = ref('');
const transcriptDetailList = ref<{ speaker: string; time: string; content: string }[]>([]);

function formatMs(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

async function openTranscriptDetail(record: Meeting) {
  transcriptDetailTitle.value = record.name;
  transcriptDetailVisible.value = true;
  transcriptDetailLoading.value = true;
  transcriptDetailList.value = [];
  try {
    const res = await meetingApi.getMeetingTranscripts(record.id, { page: 1, pageSize: 200 });
    transcriptDetailList.value = res.items.map((t) => ({
      speaker: t.speaker_name || '未知说话人',
      time: formatMs(t.start_ms),
      content: t.text,
    }));
  } catch {
    message.error('获取转写内容失败');
  } finally {
    transcriptDetailLoading.value = false;
  }
}

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
              <a-space size="small" wrap>
                <a-button v-if="record.mode === 'live'" type="link" size="small" @click="openMeetingEdit(record)">
                  <EditOutlined />编辑
                </a-button>
                <a-button type="link" size="small" @click="goMeetingDetail(record)">
                  <EyeOutlined />详情
                </a-button>
                <a-button v-if="record.mode === 'audio'" type="link" size="small" @click="openTranscriptDetail(record)">
                  <PlayCircleOutlined />查看转写
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

    <!-- 新增/编辑弹窗 -->
    <a-modal
      v-model:open="meetingModalVisible"
      title="编辑会议"
      @ok="handleMeetingSubmit"
      :confirm-loading="meetingSubmitting"
      ok-text="保存"
      cancel-text="取消"
      width="560px"
    >
      <a-form :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
        <a-form-item label="会议名称" required>
          <a-input v-model:value="meetingForm.name" placeholder="请输入会议名称" />
        </a-form-item>
        <a-form-item label="参会人员">
          <a-input v-model:value="meetingForm.participants" placeholder="请输入参会人员，多个用逗号分隔" />
        </a-form-item>
        <a-form-item label="说话人ID">
          <a-input v-model:value="meetingForm.speakerIds" placeholder="说话人ID，多个用逗号分隔" />
        </a-form-item>
        <a-form-item label="热词库ID">
          <a-input v-model:value="meetingForm.hotWordLibraryIds" placeholder="热词库ID，多个用逗号分隔" />
        </a-form-item>
        <a-form-item label="开始时间" required>
          <a-input v-model:value="meetingForm.startTime" placeholder="2026-08-10 09:00:00" />
        </a-form-item>
        <a-form-item label="结束时间" required>
          <a-input v-model:value="meetingForm.endTime" placeholder="2026-08-10 10:00:00" />
        </a-form-item>
        <a-form-item v-if="meetingEditingId" label="状态">
          <a-select v-model:value="meetingForm.status">
            <a-select-option v-for="s in store.statusOptions" :key="s" :value="s">{{ s }}</a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 转写文本详情弹窗 -->
    <a-modal
      v-model:open="transcriptDetailVisible"
      :title="`转写文本 · ${transcriptDetailTitle}`"
      :footer="null"
      width="640px"
    >
      <a-spin :spinning="transcriptDetailLoading">
        <div v-if="transcriptDetailList.length" class="transcript-list">
          <div v-for="(item, idx) in transcriptDetailList" :key="idx" class="transcript-item">
            <span class="transcript-speaker">{{ item.speaker }}</span>
            <span class="transcript-time">{{ item.time }}</span>
            <span class="transcript-content">{{ item.content }}</span>
          </div>
        </div>
        <a-empty v-else description="暂无转写内容" />
      </a-spin>
    </a-modal>
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

/* ---- 弹窗表单 ---- */
.modal-form {
  margin-top: 16px;
}
.modal-form :deep(.ant-form-item-label > label) {
  color: var(--color-text);
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

/* ---- 转写文本列表 ---- */
.transcript-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 60vh;
  overflow-y: auto;
  padding-right: 4px;
}
.transcript-item {
  display: flex;
  gap: 12px;
  align-items: baseline;
  padding: 10px 12px;
  background: var(--color-surface-2);
  border-radius: var(--radius-md);
  line-height: 1.7;
}
.transcript-speaker {
  flex: 0 0 auto;
  font-weight: 600;
  color: var(--color-brand);
  max-width: 90px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.transcript-time {
  flex: 0 0 auto;
  font-variant-numeric: tabular-nums;
  color: var(--color-text-secondary);
  width: 52px;
}
.transcript-content {
  flex: 1 1 auto;
}
</style>
