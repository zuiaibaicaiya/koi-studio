<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { App } from 'antdv-next';
const { message, modal } = App.useApp();
import type { UploadProps } from 'antdv-next';
import {
  useSpeakerStore,
  type Speaker,
  type SpeakerAudio,
  type UIGender,
  type UIStatus,
} from '../../store/speaker';
import { exportToCsv, rowsFromCsv, type CsvColumn } from '../../utils/csv';
import { toWavFile } from '../../utils/audio';
import {
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
  SearchOutlined,
  DownloadOutlined,
  UploadOutlined,
  AudioOutlined,
  StopOutlined,
} from '@antdv-next/icons';

const store = useSpeakerStore();

const columns = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 70, sorter: (a: Speaker, b: Speaker) => a.id - b.id },
  { title: '姓名', dataIndex: 'name', key: 'name' },
  { title: '音频样本', key: 'audio', width: 280 },
  { title: '描述', dataIndex: 'description', key: 'description', ellipsis: true },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 185 },
  { title: '操作', key: 'action', width: 160, fixed: 'right' },
];

const csvColumns: CsvColumn[] = [
  { key: 'id', title: 'ID' },
  { key: 'name', title: '姓名' },
  { key: 'description', title: '描述' },
  { key: 'createdAt', title: '创建时间' },
];

const keyword = ref('');
const genderFilter = ref<UIGender | '' | undefined>('');
const statusFilter = ref<UIStatus | '' | undefined>('');

const pagination = computed(() => ({
  current: store.page,
  pageSize: store.pageSize,
  total: store.total,
  showSizeChanger: true,
  showTotal: (t: number) => `共 ${t} 条`,
}));

/** 关键词 / 性别 / 状态筛选后重新查询 */
function doSearch() {
  store.load({ page: 1, keyword: keyword.value, gender: genderFilter.value, status: statusFilter.value });
}

/** 分页切换时重新查询 */
function handleTableChange(pg: { current: number; pageSize: number }) {
  store.load({
    page: pg.current,
    pageSize: pg.pageSize,
    keyword: keyword.value,
    gender: genderFilter.value,
    status: statusFilter.value,
  });
}

const modalVisible = ref(false);
const editingId = ref<number | null>(null);
const submitting = ref(false);
const formState = reactive<Omit<Speaker, 'id' | 'createdAt'>>({
  name: '',
  gender: '未知',
  language: '中文',
  status: '启用',
  sampleCount: 0,
  description: '',
  audio: null,
});

// ==========================================
// 音频样本：麦克风录制 / 本地导入 / 试听
// ==========================================
/** 当前弹窗内的音频样本 */
const audioSample = ref<SpeakerAudio | null>(null);
const audioError = ref('');
const recording = ref(false);
/** 音频转码中（录音/导入后统一转 wav） */
const transcoding = ref(false);
/** 录制时长（秒） */
const recordSeconds = ref(0);
/** 0-1 归一化音量，用于音量条 */
const recordVolume = ref(0);

/** 本次弹窗会话中创建的 Blob URL，未被保存的需要释放 */
let pendingUrls: string[] = [];
/** 编辑时记录原始音频地址，取消时不应释放 */
let originalAudioUrl: string | null = null;

let mediaRecorder: MediaRecorder | null = null;
let recordStream: MediaStream | null = null;
let audioContext: AudioContext | null = null;
let analyserNode: AnalyserNode | null = null;
let sourceNode: MediaStreamAudioSourceNode | null = null;
let recordChunks: BlobPart[] = [];
let recordStartAt = 0;
let animationId = 0;
let timerId = 0;
/** 主动放弃本次录音结果（取消弹窗、切换到导入等） */
let discardRecording = false;

const MAX_AUDIO_MB = 20;

function formatDuration(sec: number) {
  const s = Math.max(0, Math.round(sec));
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`;
}
function formatSize(bytes: number) {
  if (!bytes) return '';
  return bytes < 1024 * 1024
    ? `${(bytes / 1024).toFixed(0)} KB`
    : `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** 选择当前浏览器支持的录音编码 */
function pickMimeType() {
  const candidates = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/ogg;codecs=opus'];
  return candidates.find((t) => typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported(t)) || '';
}


/** 读取音频真实时长（导入文件时使用） */
function probeDuration(url: string) {
  return new Promise<number>((resolve) => {
    const el = new Audio();
    el.preload = 'metadata';
    el.onloadedmetadata = () => resolve(Number.isFinite(el.duration) ? el.duration : 0);
    el.onerror = () => resolve(0);
    el.src = url;
  });
}

/** 设置样本，并释放被替换掉的临时地址 */
function setAudioSample(next: SpeakerAudio) {
  const prev = audioSample.value;
  if (prev && prev.url !== originalAudioUrl) {
    URL.revokeObjectURL(prev.url);
    pendingUrls = pendingUrls.filter((u) => u !== prev.url);
  }
  pendingUrls.push(next.url);
  audioSample.value = next;
}

function clearAudio() {
  const prev = audioSample.value;
  if (prev && prev.url !== originalAudioUrl) {
    URL.revokeObjectURL(prev.url);
    pendingUrls = pendingUrls.filter((u) => u !== prev.url);
  }
  audioSample.value = null;
}

/** 释放本次会话产生但未被保存的地址 */
function releasePendingUrls(keep?: string | null) {
  pendingUrls.forEach((u) => {
    if (u !== keep) URL.revokeObjectURL(u);
  });
  pendingUrls = [];
}

/** 麦克风采集 + 录音（参考实时会议创建页的采集方式） */
async function startRecord() {
  if (recording.value) return;
  audioError.value = '';
  if (typeof MediaRecorder === 'undefined') {
    audioError.value = '当前环境不支持录音';
    return;
  }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
    });
    recordStream = stream;

    const Ctor =
      window.AudioContext ||
      (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
    audioContext = new Ctor();
    if (audioContext.state === 'suspended') await audioContext.resume();
    analyserNode = audioContext.createAnalyser();
    analyserNode.fftSize = 256;
    analyserNode.smoothingTimeConstant = 0.1;
    sourceNode = audioContext.createMediaStreamSource(stream);
    sourceNode.connect(analyserNode);

    const mimeType = pickMimeType();
    recordChunks = [];
    mediaRecorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    mediaRecorder.ondataavailable = (e) => {
      if (e.data && e.data.size > 0) recordChunks.push(e.data);
    };
    mediaRecorder.onstop = () => {
      const type = mediaRecorder?.mimeType || mimeType || 'audio/webm';
      if (discardRecording) {
        discardRecording = false;
        recordChunks = [];
        teardownRecord();
        return;
      }
      const blob = new Blob(recordChunks, { type });
      recordChunks = [];
      const duration = (Date.now() - recordStartAt) / 1000;
      teardownRecord();
      if (blob.size === 0) {
        audioError.value = '未采集到音频数据，请重试';
        return;
      }
      const stamp = new Date().toISOString().replace(/[-:T]/g, '').slice(0, 14);
      void applyRecording(blob, duration, `录音_${stamp}.wav`);
    };

    recordStartAt = Date.now();
    recordSeconds.value = 0;
    recording.value = true;
    discardRecording = false;
    mediaRecorder.start();
    timerId = window.setInterval(() => {
      recordSeconds.value = Math.floor((Date.now() - recordStartAt) / 1000);
    }, 200);
    updateVolume();
  } catch (err) {
    console.error('Microphone access error:', err);
    const e = err as { name?: string; message?: string };
    audioError.value =
      e?.name === 'NotAllowedError'
        ? '麦克风权限被拒绝，请在系统设置中允许访问'
        : e?.message || '麦克风启动失败';
    teardownRecord();
  }
}

function stopRecord() {
  if (!recording.value || !mediaRecorder) return;
  recording.value = false;
  // 数据落盘与资源释放在 onstop 回调中处理
  mediaRecorder.stop();
}

/** 释放采集链路资源 */
function teardownRecord() {
  recording.value = false;
  cancelAnimationFrame(animationId);
  if (timerId) {
    clearInterval(timerId);
    timerId = 0;
  }
  if (sourceNode) {
    sourceNode.disconnect();
    sourceNode = null;
  }
  if (recordStream) {
    recordStream.getTracks().forEach((t) => t.stop());
    recordStream = null;
  }
  if (audioContext && audioContext.state !== 'closed') {
    audioContext.close().catch(() => {});
  }
  audioContext = null;
  analyserNode = null;
  mediaRecorder = null;
  recordVolume.value = 0;
}

/** 音量采集循环 */
function updateVolume() {
  if (!recording.value || !analyserNode) return;
  const dataArray = new Uint8Array(analyserNode.frequencyBinCount);
  analyserNode.getByteTimeDomainData(dataArray);
  let sumSquares = 0;
  for (let i = 0; i < dataArray.length; i++) {
    const normalized = (dataArray[i] - 128) / 128;
    sumSquares += normalized * normalized;
  }
  const rms = Math.sqrt(sumSquares / dataArray.length);
  const expanded = Math.min(Math.pow(rms, 0.28), 1);
  recordVolume.value = recordVolume.value * 0.3 + expanded * 0.7;
  animationId = requestAnimationFrame(updateVolume);
}

/** 录音结束后转码为 WAV（后端声纹服务只接受 wav），再作为待提交样本 */
async function applyRecording(blob: Blob, duration: number, fileName: string) {
  transcoding.value = true;
  try {
    const wav = await toWavFile(blob, fileName);
    setAudioSample({
      name: wav.name,
      url: URL.createObjectURL(wav),
      duration,
      size: wav.size,
      source: 'record',
      blob: wav,
    });
    message.success(`录制完成，时长 ${formatDuration(duration)}`);
  } catch (e: unknown) {
    audioError.value = (e as { message?: string })?.message || '录音转码失败，请重试';
  } finally {
    transcoding.value = false;
  }
}

/** 导入本地音频文件 */
const beforeAudioUpload: UploadProps['beforeUpload'] = async (file) => {
  const f = file as File;
  if (!f.type.startsWith('audio/')) {
    message.warning('请选择音频文件');
    return false;
  }
  if (f.size > MAX_AUDIO_MB * 1024 * 1024) {
    message.warning(`音频文件不能超过 ${MAX_AUDIO_MB}MB`);
    return false;
  }
  if (recording.value) {
    discardRecording = true;
    stopRecord();
  }
  audioError.value = '';
  transcoding.value = true;
  try {
    const probeUrl = URL.createObjectURL(f);
    const duration = await probeDuration(probeUrl);
    URL.revokeObjectURL(probeUrl);
    // 非 wav 文件统一转码，保证后端可解析
    const wav = await toWavFile(f, f.name);
    setAudioSample({
      name: wav.name,
      url: URL.createObjectURL(wav),
      duration,
      size: wav.size,
      source: 'import',
      blob: wav,
    });
    message.success('音频导入成功');
  } catch (e: unknown) {
    audioError.value = (e as { message?: string })?.message || '音频解析失败，请更换文件';
  } finally {
    transcoding.value = false;
  }
  return false;
};

function resetForm() {
  Object.assign(formState, { name: '', gender: '未知', language: '中文', status: '启用', sampleCount: 0, description: '', audio: null });
}
function openCreate() {
  if (recording.value) {
    discardRecording = true;
    stopRecord();
  }
  releasePendingUrls();
  editingId.value = null;
  originalAudioUrl = null;
  audioSample.value = null;
  audioError.value = '';
  resetForm();
  modalVisible.value = true;
}
function openEdit(record: Speaker) {
  if (recording.value) {
    discardRecording = true;
    stopRecord();
  }
  releasePendingUrls();
  editingId.value = record.id;
  originalAudioUrl = null;
  audioSample.value = null;
  audioError.value = '';
  Object.assign(formState, {
    name: record.name,
    gender: record.gender,
    language: record.language ?? '中文',
    status: record.status ?? '启用',
    sampleCount: record.sampleCount ?? 0,
    description: record.description,
    audio: null,
  });
  modalVisible.value = true;
}
async function handleSubmit() {
  if (recording.value) {
    message.warning('请先结束录制');
    return;
  }
  if (!formState.name.trim()) {
    message.warning('请填写姓名');
    return;
  }
  if (transcoding.value) {
    message.warning('音频处理中，请稍候');
    return;
  }
  if (!audioSample.value && !editingId.value) {
    message.error('请先录制或上传音频样本');
    return;
  }
  submitting.value = true;
  try {
    const audio = audioSample.value;
    const payload = {
      name: formState.name.trim(),
      gender: formState.gender,
      description: formState.description.trim(),
      status: formState.status,
      // 录音随表单一起以 multipart/form-data 提交
      audio: audio?.blob
        ? { blob: audio.blob, fileName: audio.name, remark: audio.remark || audio.name }
        : undefined,
    };
    if (editingId.value) {
      await store.update(editingId.value, payload);
      message.success('更新成功');
    } else {
      await store.add(payload);
      message.success('注册成功');
    }
    releasePendingUrls(audio?.url);
    audioSample.value = null;
    originalAudioUrl = null;
    modalVisible.value = false;
  } catch (e: unknown) {
    message.error((e as { message?: string })?.message || '保存失败');
  } finally {
    submitting.value = false;
  }
}
function handleCancel() {
  if (recording.value) {
    discardRecording = true;
    stopRecord();
  } else {
    teardownRecord();
  }
  releasePendingUrls();
  audioSample.value = null;
  audioError.value = '';
  originalAudioUrl = null;
  modalVisible.value = false;
}
async function handleDelete(id: number) {
  try {
    await store.remove(id);
    message.success('删除成功');
  } catch (e: unknown) {
    message.error((e as { message?: string })?.message || '删除失败');
  }
}

onMounted(() => {
  store.load();
});

onBeforeUnmount(() => {
  if (recording.value) {
    discardRecording = true;
    stopRecord();
  }
  teardownRecord();
  releasePendingUrls();
});
async function handleRefresh() {
  keyword.value = '';
  genderFilter.value = '';
  statusFilter.value = '';
  await store.load({ page: 1 });
  message.success('已刷新');
}
function handleExport() {
  exportToCsv('说话人数据.csv', csvColumns, store.list as unknown as Record<string, unknown>[]);
  message.success(`已导出 ${store.list.length} 条数据`);
}

const beforeUpload: UploadProps['beforeUpload'] = (file) => {
  const reader = new FileReader();
  reader.onload = async () => {
    const text = String(reader.result || '');
    const rows = rowsFromCsv<Speaker>(text, csvColumns).map((r) => ({
      name: r.name || '',
      description: r.description || '',
    }));
    if (rows.length === 0) {
      message.warning('未解析到有效数据');
      return;
    }
    try {
      await store.importRows(rows);
      message.success(`成功导入 ${rows.length} 条数据`);
    } catch (e: unknown) {
      message.error((e as { message?: string })?.message || '导入失败');
    }
  };
  reader.readAsText(file);
  return false;
};

function confirmDelete(record: Speaker) {
  modal.confirm({
    title: '确认删除该说话人？',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: () => handleDelete(record.id),
  });
}
</script>

<template>
  <div class="page">
    <a-card class="toolbar" variant="borderless">
      <a-form layout="inline" class="filter-form">
        <a-form-item label="关键词">
          <a-input-search
            v-model:value="keyword"
            placeholder="姓名 / 描述"
            allow-clear
            style="width: 220px"
            @search="doSearch"
          >
            <template #prefix><SearchOutlined /></template>
          </a-input-search>
        </a-form-item>
        <a-form-item label="性别">
          <a-select
            v-model:value="genderFilter"
            placeholder="全部"
            allow-clear
            style="width: 120px"
            @change="doSearch"
          >
            <a-select-option v-for="g in store.genderOptions" :key="g" :value="g">{{ g }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="状态">
          <a-select
            v-model:value="statusFilter"
            placeholder="全部"
            allow-clear
            style="width: 120px"
            @change="doSearch"
          >
            <a-select-option v-for="s in store.statusOptions" :key="s" :value="s">{{ s }}</a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
      <div class="actions">
        <a-button type="primary" @click="openCreate"><PlusOutlined />新增</a-button>
        <a-upload :before-upload="beforeUpload" :show-upload-list="false" accept=".csv">
          <a-button><UploadOutlined />导入</a-button>
        </a-upload>
        <a-button @click="handleExport"><DownloadOutlined />导出</a-button>
        <a-button @click="handleRefresh"><ReloadOutlined />刷新</a-button>
      </div>
    </a-card>

    <a-card variant="borderless" class="table-card">
      <a-table
        :columns="columns"
        :data-source="store.list"
        :loading="store.loading"
        :pagination="pagination"
        row-key="id"
        :scroll="{ x: 'max-content' }"
        @change="handleTableChange"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'audio'">
            <a-tag v-if="(record.sampleCount ?? 0) > 0" color="blue">{{ record.sampleCount }} 段声纹</a-tag>
            <span v-else class="row-empty">—</span>
          </template>
          <template v-else-if="column.key === 'action'">
            <a-space size="small">
              <a-button type="link" size="small" @click="openEdit(record)">
                <EditOutlined />编辑
              </a-button>
              <a-button type="link" size="small" danger @click="confirmDelete(record)">
                <DeleteOutlined />删除
              </a-button>
            </a-space>
          </template>
        </template>
      </a-table>
    </a-card>

    <a-modal
      v-model:open="modalVisible"
      :title="editingId ? '编辑说话人' : '新增说话人'"
      :width="600"
      :mask-closable="false"
      @ok="handleSubmit"
      @cancel="handleCancel"
      :confirm-loading="submitting"
      ok-text="保存"
      cancel-text="取消"
    >
      <a-form :label-col="{ span: 5 }" :wrapper-col="{ span: 17 }" class="modal-form">
        <a-form-item label="姓名" required>
          <a-input v-model:value="formState.name" placeholder="请输入姓名" />
        </a-form-item>
        <a-form-item label="性别">
          <a-select v-model:value="formState.gender" style="width: 160px">
            <a-select-option v-for="g in store.genderOptions" :key="g" :value="g">{{ g }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="状态">
          <a-select v-model:value="formState.status" style="width: 160px">
            <a-select-option v-for="s in store.statusOptions" :key="s" :value="s">{{ s }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="音频样本">
          <div class="audio-panel">
            <div class="audio-actions">
              <a-button v-if="!recording" type="primary" ghost @click="startRecord">
                <AudioOutlined />开始录制
              </a-button>
              <a-button v-else danger type="primary" @click="stopRecord">
                <StopOutlined />结束录制
              </a-button>
              <a-upload
                :before-upload="beforeAudioUpload"
                :show-upload-list="false"
                accept="audio/*"
              >
                <a-button :disabled="recording"><UploadOutlined />导入音频</a-button>
              </a-upload>
            </div>

            <!-- 录制中：实时音量 + 计时 -->
            <div v-if="recording" class="record-live">
              <span class="rec-dot" />
              <span class="rec-time">{{ formatDuration(recordSeconds) }}</span>
              <div class="volume-meter">
                <div
                  class="volume-meter-fill"
                  :style="{
                    width: recordVolume * 100 + '%',
                    background: `linear-gradient(90deg, hsl(${210 - recordVolume * 210}, 85%, 55%), hsl(${210 - recordVolume * 210}, 85%, 65%))`,
                  }"
                ></div>
              </div>
            </div>

            <p v-if="transcoding" class="audio-hint">音频转码中（转为 wav）…</p>
            <p v-if="audioError" class="audio-error">{{ audioError }}</p>

            <!-- 已有样本：试听 -->
            <div v-if="audioSample" class="audio-preview">
              <div class="audio-meta">
                <a-tag :color="audioSample.source === 'record' ? 'blue' : 'green'">
                  {{ audioSample.source === 'record' ? '录制' : '导入' }}
                </a-tag>
                <span class="audio-name" :title="audioSample.name">{{ audioSample.name }}</span>
                <span class="audio-sub">
                  {{ formatDuration(audioSample.duration) }}
                  <template v-if="audioSample.size"> · {{ formatSize(audioSample.size) }}</template>
                </span>
                <a-button type="link" size="small" danger @click="clearAudio">
                  <DeleteOutlined />移除
                </a-button>
              </div>
              <audio :src="audioSample.url" controls class="audio-player" />
            </div>

            <p class="audio-hint">
              支持麦克风录制或导入本地音频（mp3 / wav / m4a 等，≤ {{ MAX_AUDIO_MB }}MB），提交前会自动转为 16kHz 单声道 wav，建议
              5 秒以上清晰人声，录制后可直接试听
            </p>
          </div>
        </a-form-item>
        <a-form-item label="描述">
          <a-textarea v-model:value="formState.description" :rows="3" placeholder="请输入描述" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
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
.table-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
}
.modal-form {
  margin-top: 16px;
}
.modal-form :deep(.ant-form-item-label > label) {
  color: var(--color-text);
}

/* ---- 表格内音频 ---- */
.row-audio {
  width: 240px;
  height: 32px;
}
.row-empty {
  color: var(--color-text-muted);
}

/* ---- 音频样本面板 ---- */
.audio-panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.audio-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.record-live {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md, 8px);
  background: var(--color-surface-2, #fafafa);
}
.rec-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #ff4d4f;
  flex-shrink: 0;
  animation: rec-blink 1.2s ease-in-out infinite;
}
@keyframes rec-blink {
  0%, 100% { opacity: 1; box-shadow: 0 0 0 0 rgba(255, 77, 79, 0.5); }
  50% { opacity: 0.45; box-shadow: 0 0 0 5px rgba(255, 77, 79, 0); }
}
.rec-time {
  font-size: 13px;
  font-variant-numeric: tabular-nums;
  color: var(--color-text);
  flex-shrink: 0;
}
.volume-meter {
  flex: 1;
  height: 6px;
  min-width: 60px;
  border-radius: 3px;
  background: var(--color-fill-secondary, rgba(0, 0, 0, 0.06));
  overflow: hidden;
}
.volume-meter-fill {
  height: 100%;
  border-radius: 3px;
  min-width: 2px;
  transition: width 0.08s linear;
}
.audio-error {
  margin: 0;
  font-size: 12px;
  color: #ff4d4f;
}
.audio-preview {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md, 8px);
  background: var(--color-surface-2, #fafafa);
}
.audio-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.audio-name {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: var(--color-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.audio-sub {
  font-size: 12px;
  color: var(--color-text-muted);
  white-space: nowrap;
}
.audio-player {
  width: 100%;
  height: 36px;
}
.audio-hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--color-text-muted);
}
</style>
