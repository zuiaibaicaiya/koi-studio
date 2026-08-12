<script setup lang="ts">
import { computed, reactive, ref, onMounted, watch } from 'vue';
import { App } from 'antdv-next';
import {
  useMeetingStore,
  type Meeting,
  type UIMeetingStatus,
} from '../../store/meeting';
import { exportToCsv, type CsvColumn } from '../../utils/csv';
import {
  EditOutlined,
  DeleteOutlined,
  SearchOutlined,
  ReloadOutlined,
  DownloadOutlined,
  VideoCameraOutlined,
  AudioOutlined,
  EyeOutlined,
  PlayCircleOutlined,
  StopOutlined,
} from '@antdv-next/icons';

// 使用 App 上下文的 message / modal 实例，使其继承 ConfigProvider 主题（暗黑模式），
// 避免出现静态方法渲染到 body、不跟随主题变化的问题。
const { message, modal } = App.useApp();

const store = useMeetingStore();

/* ==================== 页签 ==================== */
const activeTab = ref<'live' | 'transcription'>('live');

/* ==================== 实时会议（对接后端） ==================== */
const meetingColumns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
  { title: '会议名称', dataIndex: 'name', key: 'name', ellipsis: true },
  { title: '参会人员', dataIndex: 'participants', key: 'participants', ellipsis: true },
  { title: '开始时间', dataIndex: 'startTime', key: 'startTime', width: 185 },
  { title: '结束时间', dataIndex: 'endTime', key: 'endTime', width: 185 },
  { title: '音频', dataIndex: 'audioUrl', key: 'audioUrl', width: 280 },
  { title: '状态', dataIndex: 'status', key: 'status', width: 100 },
  { title: '操作', key: 'action', width: 150, fixed: 'right' as const },
];

const meetingCsvColumns: CsvColumn[] = [
  { key: 'id', title: 'ID' },
  { key: 'name', title: '会议名称' },
  { key: 'participants', title: '参会人员' },
  { key: 'speakerIds', title: '说话人ID' },
  { key: 'hotWordLibraryIds', title: '热词库ID' },
  { key: 'startTime', title: '开始时间' },
  { key: 'endTime', title: '结束时间' },
  { key: 'status', title: '状态' },
];

/* ---- 搜索与分页 ---- */
const meetingKeyword = ref('');
const meetingTimeRange = ref<[string, string] | null>(null);

const meetingPagination = reactive({
  current: 1,
  pageSize: 10,
  showSizeChanger: true,
  showTotal: (t: number) => `共 ${t} 条`,
});

function doSearchMeeting() {
  meetingPagination.current = 1;
  store.load({
    page: 1,
    pageSize: meetingPagination.pageSize,
    keyword: meetingKeyword.value || undefined,
    startTime: meetingTimeRange.value?.[0] || undefined,
    endTime: meetingTimeRange.value?.[1] || undefined,
  });
}

function handleMeetingReset() {
  meetingKeyword.value = '';
  meetingTimeRange.value = null;
  doSearchMeeting();
}

function onMeetingPageChange(page: number, pageSize: number) {
  meetingPagination.current = page;
  meetingPagination.pageSize = pageSize;
  store.load({
    page,
    pageSize,
    keyword: meetingKeyword.value || undefined,
    startTime: meetingTimeRange.value?.[0] || undefined,
    endTime: meetingTimeRange.value?.[1] || undefined,
  });
}

watch(meetingKeyword, doSearchMeeting);
watch(meetingTimeRange, doSearchMeeting);

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

function handleMeetingExportOne(record: Meeting) {
  exportToCsv(`会议-${record.id}.csv`, meetingCsvColumns, [record as unknown as Record<string, unknown>]);
  message.success(`已导出会议 ${record.id}`);
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

/* ---- 开始/结束会议 ---- */
async function handleStartMeeting(record: Meeting) {
  try {
    await store.start(record.id);
    message.success(`会议「${record.name}」已开始`);
  } catch (e: unknown) {
    message.error((e as { message?: string })?.message || '操作失败');
  }
}

async function handleFinishMeeting(record: Meeting) {
  try {
    await store.finish(record.id);
    message.success(`会议「${record.name}」已结束`);
  } catch (e: unknown) {
    message.error((e as { message?: string })?.message || '操作失败');
  }
}

/* ---- 查看会议详情弹窗 ---- */
const meetingDetailVisible = ref(false);
const meetingDetail = ref<Meeting | null>(null);
function openMeetingDetail(record: Meeting) {
  meetingDetail.value = record;
  meetingDetailVisible.value = true;
}

/* ==================== 音频转写（暂时保留本地模拟数据） ==================== */
type TranscriptionStatus = '转写中' | '已完成' | '失败';

interface LocalTranscription {
  id: number;
  meetingTitle: string;
  fileName: string;
  audioUrl: string;
  language: string;
  duration: number;
  status: TranscriptionStatus;
  transcriptText: string;
  createdAt: string;
}

const transcriptionList = computed<LocalTranscription[]>(() =>
  store.transcriptions.map((t) => ({
    id: t.id,
    meetingTitle: '',
    fileName: `转写记录 ${t.id}`,
    audioUrl: '',
    language: '中文普通话',
    duration: 0,
    status: '已完成' as TranscriptionStatus,
    transcriptText: t.content,
    createdAt: t.time,
  })),
);

const transcriptionColumns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70 },
  { title: '说话人', dataIndex: 'meetingTitle', key: 'meetingTitle', ellipsis: true },
  { title: '时间', dataIndex: 'createdAt', key: 'createdAt', width: 160 },
  { title: '状态', dataIndex: 'status', key: 'status', width: 90 },
  { title: '操作', key: 'action', width: 110, fixed: 'right' as const },
];

const transcriptionKeyword = ref('');
const transcriptionStatusFilter = ref<TranscriptionStatus | undefined>();
const transcriptionPagination = reactive({ current: 1, pageSize: 10, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` });

const transcriptionStatuses: TranscriptionStatus[] = ['转写中', '已完成', '失败'];

const transcriptionFiltered = computed(() => {
  let list: LocalTranscription[] = [...transcriptionList.value];
  const kw = transcriptionKeyword.value.trim().toLowerCase();
  if (kw) list = list.filter((t) => t.transcriptText.toLowerCase().includes(kw));
  if (transcriptionStatusFilter.value) list = list.filter((t) => t.status === transcriptionStatusFilter.value);
  return list;
});

/* ---- 查看转写文本弹窗 ---- */
const transcriptDetailVisible = ref(false);
const transcriptDetail = ref<LocalTranscription | null>(null);
function openTranscriptDetail(record: LocalTranscription) {
  transcriptDetail.value = record;
  transcriptDetailVisible.value = true;
}

/* ==================== 初始化 ==================== */
onMounted(() => {
  store.load();
});
</script>

<template>
  <div class="page">
    <!-- ========== 页签切换 ========== -->
    <a-card variant="borderless" class="tab-card">
      <a-tabs v-model:activeKey="activeTab" class="meeting-tabs">
        <a-tab-pane key="live">
          <template #tab>
            <span><VideoCameraOutlined /> 实时会议</span>
          </template>
        </a-tab-pane>
        <a-tab-pane key="transcription">
          <template #tab>
            <span><AudioOutlined /> 音频转写</span>
          </template>
        </a-tab-pane>
      </a-tabs>
    </a-card>

    <!-- ==================== 实时会议 ==================== -->
    <template v-if="activeTab === 'live'">
      <!-- 工具栏 -->
      <a-card class="toolbar" variant="borderless">
        <a-form layout="inline" class="filter-form">
          <a-form-item label="关键词">
            <a-input v-model:value="meetingKeyword" placeholder="会议名称 / 参会人员" allow-clear style="width: 240px">
              <template #prefix><SearchOutlined /></template>
            </a-input>
          </a-form-item>
          <a-form-item label="时间段">
            <a-range-picker
              v-model:value="meetingTimeRange"
              :show-time="{ format: 'HH:mm:ss' }"
              format="YYYY-MM-DD HH:mm:ss"
              value-format="YYYY-MM-DD HH:mm:ss"
              :placeholder="['开始时间', '结束时间']"
              style="width: 380px"
            />
          </a-form-item>
          <a-form-item>
            <a-space>
              <a-button type="primary" @click="doSearchMeeting">
                <SearchOutlined />搜索
              </a-button>
              <a-button @click="handleMeetingReset">
                <ReloadOutlined />重置
              </a-button>
            </a-space>
          </a-form-item>
        </a-form>
      </a-card>

      <!-- 数据表格 -->
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
              <template v-if="column.key === 'status'">
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

      <!-- 会议详情弹窗 -->
      <a-modal
        v-model:open="meetingDetailVisible"
        title="会议详情"
        :footer="null"
        width="520px"
      >
        <a-descriptions v-if="meetingDetail" :column="1" bordered size="middle" class="detail-desc">
          <a-descriptions-item label="ID">{{ meetingDetail.id }}</a-descriptions-item>
          <a-descriptions-item label="会议名称">{{ meetingDetail.name }}</a-descriptions-item>
          <a-descriptions-item label="参会人员">{{ meetingDetail.participants || '--' }}</a-descriptions-item>
          <a-descriptions-item label="说话人ID">{{ meetingDetail.speakerIds || '--' }}</a-descriptions-item>
          <a-descriptions-item label="热词库ID">{{ meetingDetail.hotWordLibraryIds || '--' }}</a-descriptions-item>
          <a-descriptions-item label="开始时间">{{ meetingDetail.startTime }}</a-descriptions-item>
          <a-descriptions-item label="结束时间">{{ meetingDetail.endTime }}</a-descriptions-item>
          <a-descriptions-item label="状态">
            <a-tag :color="meetingDetail.rawStatus === 'ongoing' ? 'green' : meetingDetail.rawStatus === 'created' ? 'blue' : 'default'">
              {{ meetingDetail.status }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="音频">
            <audio v-if="meetingDetail.audioUrl" :src="meetingDetail.audioUrl" controls preload="metadata" class="audio-player" />
            <span v-else class="muted">--</span>
          </a-descriptions-item>
          <a-descriptions-item label="创建者ID">{{ meetingDetail.createdBy || '--' }}</a-descriptions-item>
          <a-descriptions-item label="创建时间">{{ meetingDetail.createdAt }}</a-descriptions-item>
        </a-descriptions>
      </a-modal>
    </template>

    <!-- ==================== 音频转写 ==================== -->
    <template v-if="activeTab === 'transcription'">
      <!-- 工具栏 -->
      <a-card class="toolbar" variant="borderless">
        <a-form layout="inline" class="filter-form">
          <a-form-item label="关键词">
            <a-input v-model:value="transcriptionKeyword" placeholder="搜索转写内容" allow-clear style="width: 260px">
              <template #prefix><SearchOutlined /></template>
            </a-input>
          </a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="transcriptionStatusFilter" placeholder="全部" allow-clear style="width: 120px">
              <a-select-option v-for="s in transcriptionStatuses" :key="s" :value="s">{{ s }}</a-select-option>
            </a-select>
          </a-form-item>
        </a-form>
        <div class="actions">
          <a-button @click="transcriptionKeyword = ''; transcriptionStatusFilter = undefined;">重置筛选</a-button>
        </div>
      </a-card>

      <!-- 数据表格 -->
      <a-card variant="borderless" class="table-card">
        <a-table
          :columns="transcriptionColumns"
          :data-source="transcriptionFiltered"
          :pagination="transcriptionPagination"
          row-key="id"
          :scroll="{ x: 'max-content' }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'status'">
              <a-tag color="green">{{ record.status }}</a-tag>
            </template>
            <template v-else-if="column.key === 'action'">
              <a-space size="small" wrap>
                <a-button type="link" size="small" @click="openTranscriptDetail(record)">
                  <EyeOutlined />查看
                </a-button>
              </a-space>
            </template>
          </template>
        </a-table>
      </a-card>

      <!-- 转写文本详情弹窗 -->
      <a-modal
        v-model:open="transcriptDetailVisible"
        title="转写文本详情"
        :footer="null"
        width="640px"
      >
        <a-descriptions v-if="transcriptDetail" :column="1" bordered size="middle" class="detail-desc">
          <a-descriptions-item label="说话人">{{ transcriptDetail.meetingTitle }}</a-descriptions-item>
          <a-descriptions-item label="时间">{{ transcriptDetail.createdAt }}</a-descriptions-item>
          <a-descriptions-item label="状态">
            <a-tag color="green">{{ transcriptDetail.status }}</a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="转写文本">
            <div class="transcript-text">{{ transcriptDetail.transcriptText || '暂无转写内容' }}</div>
          </a-descriptions-item>
        </a-descriptions>
      </a-modal>
    </template>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* ---- 页签 ---- */
.tab-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
}
.meeting-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 0;
}
.meeting-tabs :deep(.ant-tabs-tab) {
  color: var(--color-text-secondary);
  font-size: 15px;
}
.meeting-tabs :deep(.ant-tabs-tab.ant-tabs-tab-active .ant-tabs-tab-btn) {
  color: var(--color-brand);
}

/* ---- 工具栏 ---- */
.toolbar {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 12px 16px;
}
.toolbar :deep(.ant-form-item-label > label) {
  color: var(--color-text-secondary);
}
.filter-form {
  margin-bottom: 12px;
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
.audio-player {
  width: 240px;
  height: 36px;
}
.audio-player:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 2px;
}
</style>
