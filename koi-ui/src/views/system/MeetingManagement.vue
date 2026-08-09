<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { message } from 'antdv-next';
import type { UploadProps } from 'antdv-next';
import {
  useMeetingStore,
  type Meeting,
  type MeetingStatus,
  type Transcription,
  type TranscriptionStatus,
} from '../../store/meeting';
import { exportToCsv, rowsFromCsv, type CsvColumn } from '../../utils/csv';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
  SearchOutlined,
  DownloadOutlined,
  UploadOutlined,
  FileExcelOutlined,
  VideoCameraOutlined,
  AudioOutlined,
  PlayCircleOutlined,
  EyeOutlined,
  LoadingOutlined,
} from '@antdv-next/icons';

const store = useMeetingStore();

/* ==================== 页签 ==================== */
const activeTab = ref<'live' | 'transcription'>('live');

/* ==================== 实时会议 ==================== */
const meetingColumns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70, sorter: (a: Meeting, b: Meeting) => a.id - b.id },
  { title: '会议标题', dataIndex: 'title', key: 'title', sorter: (a: Meeting, b: Meeting) => a.title.localeCompare(b.title) },
  { title: '主持人', dataIndex: 'host', key: 'host', width: 100 },
  { title: '开始时间', dataIndex: 'startTime', key: 'startTime', width: 160 },
  { title: '结束时间', dataIndex: 'endTime', key: 'endTime', width: 160 },
  { title: '状态', dataIndex: 'status', key: 'status', width: 90 },
  { title: '参会人数', dataIndex: 'participants', key: 'participants', width: 90, sorter: (a: Meeting, b: Meeting) => a.participants - b.participants },
  { title: '操作', key: 'action', width: 180, fixed: 'right' },
];

const meetingCsvColumns: CsvColumn[] = [
  { key: 'id', title: 'ID' },
  { key: 'title', title: '会议标题' },
  { key: 'host', title: '主持人' },
  { key: 'roomUrl', title: '会议室链接' },
  { key: 'startTime', title: '开始时间' },
  { key: 'endTime', title: '结束时间' },
  { key: 'status', title: '状态' },
  { key: 'participants', title: '参会人数' },
  { key: 'description', title: '会议描述' },
];

const meetingKeyword = ref('');
const meetingStatusFilter = ref<MeetingStatus | undefined>();
const meetingPagination = reactive({ current: 1, pageSize: 10, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` });

const meetingStatuses: MeetingStatus[] = ['进行中', '已结束', '已预约'];

const meetingFiltered = computed(() =>
  store.meetings.filter((m) => {
    const kw = meetingKeyword.value.trim().toLowerCase();
    const matchKw = !kw || m.title.toLowerCase().includes(kw) || m.host.toLowerCase().includes(kw) || m.description.toLowerCase().includes(kw);
    const matchStatus = !meetingStatusFilter.value || m.status === meetingStatusFilter.value;
    return matchKw && matchStatus;
  }),
);

/* ---- 会议弹窗 ---- */
const meetingModalVisible = ref(false);
const meetingEditingId = ref<number | null>(null);
const meetingSubmitting = ref(false);
const meetingForm = reactive<Omit<Meeting, 'id'>>({
  title: '',
  host: '',
  roomUrl: '',
  startTime: '',
  endTime: '',
  status: '已预约',
  participants: 0,
  description: '',
});

function resetMeetingForm() {
  Object.assign(meetingForm, {
    title: '', host: '', roomUrl: '', startTime: '', endTime: '',
    status: '已预约' as MeetingStatus, participants: 0, description: '',
  });
}

function openMeetingCreate() {
  meetingEditingId.value = null;
  resetMeetingForm();
  meetingModalVisible.value = true;
}

function openMeetingEdit(record: Meeting) {
  meetingEditingId.value = record.id;
  Object.assign(meetingForm, {
    title: record.title, host: record.host, roomUrl: record.roomUrl,
    startTime: record.startTime, endTime: record.endTime,
    status: record.status, participants: record.participants, description: record.description,
  });
  meetingModalVisible.value = true;
}

function handleMeetingSubmit() {
  if (!meetingForm.title.trim()) { message.warning('请输入会议标题'); return; }
  if (!meetingForm.startTime.trim() || !meetingForm.endTime.trim()) { message.warning('请选择开始和结束时间'); return; }
  meetingSubmitting.value = true;
  if (meetingEditingId.value) {
    store.updateMeeting(meetingEditingId.value, { ...meetingForm });
    message.success('会议更新成功');
  } else {
    store.addMeeting({ ...meetingForm });
    message.success('会议创建成功');
  }
  meetingSubmitting.value = false;
  meetingModalVisible.value = false;
}

function handleMeetingDelete(id: number) {
  store.removeMeeting(id);
  message.success('会议已删除');
}

function handleMeetingRefresh() {
  meetingKeyword.value = '';
  meetingStatusFilter.value = undefined;
  meetingPagination.current = 1;
  message.success('已刷新');
}

function handleMeetingExport() {
  exportToCsv('会议数据.csv', meetingCsvColumns, store.meetings as unknown as Record<string, unknown>[]);
  message.success(`已导出 ${store.meetings.length} 条数据`);
}

const meetingBeforeUpload: UploadProps['beforeUpload'] = (file) => {
  const reader = new FileReader();
  reader.onload = () => {
    const text = String(reader.result || '');
    const rows = rowsFromCsv<Meeting>(text, meetingCsvColumns).map((r) => ({
      title: r.title || '',
      host: r.host || '',
      roomUrl: r.roomUrl || '',
      startTime: r.startTime || '',
      endTime: r.endTime || '',
      status: (r.status as MeetingStatus) || '已预约',
      participants: Number(r.participants) || 0,
      description: r.description || '',
    }));
    if (rows.length === 0) { message.warning('未解析到有效数据'); return; }
    store.importMeetings(rows as Meeting[]);
    message.success(`成功导入 ${rows.length} 条数据`);
  };
  reader.readAsText(file);
  return false;
};

/* ---- 查看会议详情弹窗 ---- */
const meetingDetailVisible = ref(false);
const meetingDetail = ref<Meeting | null>(null);
function openMeetingDetail(record: Meeting) {
  meetingDetail.value = record;
  meetingDetailVisible.value = true;
}

/* ==================== 音频转写 ==================== */
const transcriptionColumns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70, sorter: (a: Transcription, b: Transcription) => a.id - b.id },
  { title: '关联会议', dataIndex: 'meetingTitle', key: 'meetingTitle' },
  { title: '文件名', dataIndex: 'fileName', key: 'fileName' },
  { title: '语言', dataIndex: 'language', key: 'language', width: 110 },
  { title: '时长', dataIndex: 'duration', key: 'duration', width: 90, sorter: (a: Transcription, b: Transcription) => a.duration - b.duration },
  { title: '状态', dataIndex: 'status', key: 'status', width: 90 },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 120 },
  { title: '操作', key: 'action', width: 200, fixed: 'right' },
];

const transcriptionCsvColumns: CsvColumn[] = [
  { key: 'id', title: 'ID' },
  { key: 'meetingTitle', title: '关联会议' },
  { key: 'fileName', title: '文件名' },
  { key: 'audioUrl', title: '音频链接' },
  { key: 'status', title: '状态' },
  { key: 'duration', title: '时长(秒)' },
  { key: 'language', title: '语言' },
  { key: 'transcriptText', title: '转写文本' },
  { key: 'createdAt', title: '创建时间' },
];

const transcriptionKeyword = ref('');
const transcriptionStatusFilter = ref<TranscriptionStatus | undefined>();
const transcriptionLanguageFilter = ref<string | undefined>();
const transcriptionPagination = reactive({ current: 1, pageSize: 10, showSizeChanger: true, showTotal: (t: number) => `共 ${t} 条` });

const transcriptionStatuses: TranscriptionStatus[] = ['转写中', '已完成', '失败'];
const transcriptionLanguages = ['中文普通话', '英文', '粤语', '日语', '韩语'];

const transcriptionFiltered = computed(() =>
  store.transcriptions.filter((t) => {
    const kw = transcriptionKeyword.value.trim().toLowerCase();
    const matchKw = !kw || t.meetingTitle.toLowerCase().includes(kw) || t.fileName.toLowerCase().includes(kw) || t.transcriptText.toLowerCase().includes(kw);
    const matchStatus = !transcriptionStatusFilter.value || t.status === transcriptionStatusFilter.value;
    const matchLang = !transcriptionLanguageFilter.value || t.language === transcriptionLanguageFilter.value;
    return matchKw && matchStatus && matchLang;
  }),
);

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
  return `${m}:${String(s).padStart(2, '0')}`;
}

/* ---- 转写弹窗 ---- */
const transModalVisible = ref(false);
const transEditingId = ref<number | null>(null);
const transSubmitting = ref(false);
const transForm = reactive<Omit<Transcription, 'id' | 'createdAt'>>({
  meetingTitle: '',
  fileName: '',
  audioUrl: '',
  status: '转写中' as TranscriptionStatus,
  duration: 0,
  language: '中文普通话',
  transcriptText: '',
});

function resetTransForm() {
  Object.assign(transForm, {
    meetingTitle: '', fileName: '', audioUrl: '',
    status: '转写中' as TranscriptionStatus, duration: 0,
    language: '中文普通话', transcriptText: '',
  });
}

function openTransCreate() {
  transEditingId.value = null;
  resetTransForm();
  transModalVisible.value = true;
}

function openTransEdit(record: Transcription) {
  transEditingId.value = record.id;
  Object.assign(transForm, {
    meetingTitle: record.meetingTitle, fileName: record.fileName, audioUrl: record.audioUrl,
    status: record.status, duration: record.duration, language: record.language, transcriptText: record.transcriptText,
  });
  transModalVisible.value = true;
}

function handleTransSubmit() {
  if (!transForm.meetingTitle.trim()) { message.warning('请输入关联会议标题'); return; }
  if (!transForm.fileName.trim()) { message.warning('请输入文件名'); return; }
  transSubmitting.value = true;
  if (transEditingId.value) {
    store.updateTranscription(transEditingId.value, { ...transForm });
    message.success('转写记录更新成功');
  } else {
    store.addTranscription({ ...transForm });
    message.success('转写记录创建成功');
  }
  transSubmitting.value = false;
  transModalVisible.value = false;
}

function handleTransDelete(id: number) {
  store.removeTranscription(id);
  message.success('转写记录已删除');
}

function handleTransRefresh() {
  transcriptionKeyword.value = '';
  transcriptionStatusFilter.value = undefined;
  transcriptionLanguageFilter.value = undefined;
  transcriptionPagination.current = 1;
  message.success('已刷新');
}

function handleTransExport() {
  exportToCsv('音频转写数据.csv', transcriptionCsvColumns, store.transcriptions as unknown as Record<string, unknown>[]);
  message.success(`已导出 ${store.transcriptions.length} 条数据`);
}

const transBeforeUpload: UploadProps['beforeUpload'] = (file) => {
  const reader = new FileReader();
  reader.onload = () => {
    const text = String(reader.result || '');
    const rows = rowsFromCsv<Transcription>(text, transcriptionCsvColumns).map((r) => ({
      meetingTitle: r.meetingTitle || '',
      fileName: r.fileName || '',
      audioUrl: r.audioUrl || '',
      status: (r.status as TranscriptionStatus) || '转写中',
      duration: Number(r.duration) || 0,
      language: r.language || '中文普通话',
      transcriptText: r.transcriptText || '',
    }));
    if (rows.length === 0) { message.warning('未解析到有效数据'); return; }
    store.importTranscriptions(rows as Transcription[]);
    message.success(`成功导入 ${rows.length} 条数据`);
  };
  reader.readAsText(file);
  return false;
};

/* ---- 查看转写文本弹窗 ---- */
const transcriptDetailVisible = ref(false);
const transcriptDetail = ref<Transcription | null>(null);
function openTranscriptDetail(record: Transcription) {
  transcriptDetail.value = record;
  transcriptDetailVisible.value = true;
}
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
            <a-input v-model:value="meetingKeyword" placeholder="标题 / 主持人 / 描述" allow-clear style="width: 240px">
              <template #prefix><SearchOutlined /></template>
            </a-input>
          </a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="meetingStatusFilter" placeholder="全部" allow-clear style="width: 130px">
              <a-select-option v-for="s in meetingStatuses" :key="s" :value="s">{{ s }}</a-select-option>
            </a-select>
          </a-form-item>
        </a-form>
        <div class="actions">
          <a-button type="primary" @click="openMeetingCreate"><PlusOutlined />新增会议</a-button>
          <a-upload :before-upload="meetingBeforeUpload" :show-upload-list="false" accept=".csv">
            <a-button><UploadOutlined />导入</a-button>
          </a-upload>
          <a-button @click="handleMeetingExport"><DownloadOutlined />导出</a-button>
          <a-button @click="handleMeetingRefresh"><ReloadOutlined />刷新</a-button>
          <a-button type="link" @click="handleMeetingExport"><FileExcelOutlined />下载模板</a-button>
        </div>
      </a-card>

      <!-- 数据表格 -->
      <a-card variant="borderless" class="table-card">
        <a-table
          :columns="meetingColumns"
          :data-source="meetingFiltered"
          :pagination="meetingPagination"
          row-key="id"
          :scroll="{ x: 1000 }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'status'">
              <a-tag :color="record.status === '进行中' ? 'green' : record.status === '已预约' ? 'blue' : 'default'">
                <template v-if="record.status === '进行中'">
                  <span class="status-dot dot-active" />
                </template>
                {{ record.status }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'action'">
              <a-space>
                <a-button type="link" size="small" @click="openMeetingDetail(record)">
                  <EyeOutlined />详情
                </a-button>
                <a-button type="link" size="small" @click="openMeetingEdit(record)">
                  <EditOutlined />编辑
                </a-button>
                <a-popconfirm title="确认删除该会议？" @confirm="handleMeetingDelete(record.id)">
                  <a-button type="link" size="small" danger>
                    <DeleteOutlined />删除
                  </a-button>
                </a-popconfirm>
              </a-space>
            </template>
          </template>
        </a-table>
      </a-card>

      <!-- 新增/编辑弹窗 -->
      <a-modal
        v-model:open="meetingModalVisible"
        :title="meetingEditingId ? '编辑会议' : '新增会议'"
        @ok="handleMeetingSubmit"
        :confirm-loading="meetingSubmitting"
        ok-text="保存"
        cancel-text="取消"
        width="560px"
      >
        <a-form :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
          <a-form-item label="会议标题" required>
            <a-input v-model:value="meetingForm.title" placeholder="请输入会议标题" />
          </a-form-item>
          <a-form-item label="主持人">
            <a-input v-model:value="meetingForm.host" placeholder="请输入主持人" />
          </a-form-item>
          <a-form-item label="会议室链接">
            <a-input v-model:value="meetingForm.roomUrl" placeholder="请输入会议室链接" />
          </a-form-item>
          <a-form-item label="开始时间" required>
            <a-input v-model:value="meetingForm.startTime" placeholder="2025-01-01 09:00" />
          </a-form-item>
          <a-form-item label="结束时间" required>
            <a-input v-model:value="meetingForm.endTime" placeholder="2025-01-01 10:00" />
          </a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="meetingForm.status">
              <a-select-option v-for="s in meetingStatuses" :key="s" :value="s">{{ s }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="参会人数">
            <a-input-number v-model:value="meetingForm.participants" :min="0" style="width:100%" />
          </a-form-item>
          <a-form-item label="描述">
            <a-textarea v-model:value="meetingForm.description" placeholder="请输入会议描述" :rows="3" />
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
          <a-descriptions-item label="会议标题">{{ meetingDetail.title }}</a-descriptions-item>
          <a-descriptions-item label="主持人">{{ meetingDetail.host }}</a-descriptions-item>
          <a-descriptions-item label="会议室链接">
            <a :href="meetingDetail.roomUrl" target="_blank">{{ meetingDetail.roomUrl }}</a>
          </a-descriptions-item>
          <a-descriptions-item label="开始时间">{{ meetingDetail.startTime }}</a-descriptions-item>
          <a-descriptions-item label="结束时间">{{ meetingDetail.endTime }}</a-descriptions-item>
          <a-descriptions-item label="状态">
            <a-tag :color="meetingDetail.status === '进行中' ? 'green' : meetingDetail.status === '已预约' ? 'blue' : 'default'">
              {{ meetingDetail.status }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="参会人数">{{ meetingDetail.participants }} 人</a-descriptions-item>
          <a-descriptions-item label="描述">{{ meetingDetail.description }}</a-descriptions-item>
        </a-descriptions>
      </a-modal>
    </template>

    <!-- ==================== 音频转写 ==================== -->
    <template v-if="activeTab === 'transcription'">
      <!-- 工具栏 -->
      <a-card class="toolbar" variant="borderless">
        <a-form layout="inline" class="filter-form">
          <a-form-item label="关键词">
            <a-input v-model:value="transcriptionKeyword" placeholder="会议标题 / 文件名 / 转写内容" allow-clear style="width: 260px">
              <template #prefix><SearchOutlined /></template>
            </a-input>
          </a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="transcriptionStatusFilter" placeholder="全部" allow-clear style="width: 120px">
              <a-select-option v-for="s in transcriptionStatuses" :key="s" :value="s">{{ s }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="语言">
            <a-select v-model:value="transcriptionLanguageFilter" placeholder="全部" allow-clear style="width: 140px">
              <a-select-option v-for="l in transcriptionLanguages" :key="l" :value="l">{{ l }}</a-select-option>
            </a-select>
          </a-form-item>
        </a-form>
        <div class="actions">
          <a-button type="primary" @click="openTransCreate"><PlusOutlined />新增转写</a-button>
          <a-upload :before-upload="transBeforeUpload" :show-upload-list="false" accept=".csv">
            <a-button><UploadOutlined />导入</a-button>
          </a-upload>
          <a-button @click="handleTransExport"><DownloadOutlined />导出</a-button>
          <a-button @click="handleTransRefresh"><ReloadOutlined />刷新</a-button>
          <a-button type="link" @click="handleTransExport"><FileExcelOutlined />下载模板</a-button>
        </div>
      </a-card>

      <!-- 数据表格 -->
      <a-card variant="borderless" class="table-card">
        <a-table
          :columns="transcriptionColumns"
          :data-source="transcriptionFiltered"
          :pagination="transcriptionPagination"
          row-key="id"
          :scroll="{ x: 1050 }"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.key === 'status'">
              <a-tag :color="record.status === '已完成' ? 'green' : record.status === '转写中' ? 'processing' : 'red'">
                <template v-if="record.status === '转写中'">
                  <LoadingOutlined />
                </template>
                {{ record.status }}
              </a-tag>
            </template>
            <template v-else-if="column.key === 'duration'">
              {{ formatDuration(record.duration) }}
            </template>
            <template v-else-if="column.key === 'action'">
              <a-space>
                <a-button type="link" size="small" @click="openTranscriptDetail(record)">
                  <EyeOutlined />查看
                </a-button>
                <a-button type="link" size="small" @click="openTransEdit(record)">
                  <EditOutlined />编辑
                </a-button>
                <a-popconfirm title="确认删除该转写记录？" @confirm="handleTransDelete(record.id)">
                  <a-button type="link" size="small" danger>
                    <DeleteOutlined />删除
                  </a-button>
                </a-popconfirm>
              </a-space>
            </template>
          </template>
        </a-table>
      </a-card>

      <!-- 新增/编辑转写弹窗 -->
      <a-modal
        v-model:open="transModalVisible"
        :title="transEditingId ? '编辑转写记录' : '新增转写记录'"
        @ok="handleTransSubmit"
        :confirm-loading="transSubmitting"
        ok-text="保存"
        cancel-text="取消"
        width="560px"
      >
        <a-form :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
          <a-form-item label="关联会议" required>
            <a-input v-model:value="transForm.meetingTitle" placeholder="请输入关联会议标题" />
          </a-form-item>
          <a-form-item label="文件名" required>
            <a-input v-model:value="transForm.fileName" placeholder="请输入音频文件名" />
          </a-form-item>
          <a-form-item label="音频链接">
            <a-input v-model:value="transForm.audioUrl" placeholder="请输入音频文件链接" />
          </a-form-item>
          <a-form-item label="语言">
            <a-select v-model:value="transForm.language">
              <a-select-option v-for="l in transcriptionLanguages" :key="l" :value="l">{{ l }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="时长(秒)">
            <a-input-number v-model:value="transForm.duration" :min="0" style="width:100%" placeholder="音频时长，单位秒" />
          </a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="transForm.status">
              <a-select-option v-for="s in transcriptionStatuses" :key="s" :value="s">{{ s }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="转写文本">
            <a-textarea v-model:value="transForm.transcriptText" placeholder="请输入或修改转写文本" :rows="4" />
          </a-form-item>
        </a-form>
      </a-modal>

      <!-- 转写文本详情弹窗 -->
      <a-modal
        v-model:open="transcriptDetailVisible"
        title="转写文本详情"
        :footer="null"
        width="640px"
      >
        <a-descriptions v-if="transcriptDetail" :column="1" bordered size="middle" class="detail-desc">
          <a-descriptions-item label="关联会议">{{ transcriptDetail.meetingTitle }}</a-descriptions-item>
          <a-descriptions-item label="文件名">{{ transcriptDetail.fileName }}</a-descriptions-item>
          <a-descriptions-item label="语言">{{ transcriptDetail.language }}</a-descriptions-item>
          <a-descriptions-item label="时长">{{ formatDuration(transcriptDetail.duration) }}</a-descriptions-item>
          <a-descriptions-item label="状态">
            <a-tag :color="transcriptDetail.status === '已完成' ? 'green' : transcriptDetail.status === '转写中' ? 'processing' : 'red'">
              {{ transcriptDetail.status }}
            </a-tag>
          </a-descriptions-item>
          <a-descriptions-item label="创建时间">{{ transcriptDetail.createdAt }}</a-descriptions-item>
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
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
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
</style>
