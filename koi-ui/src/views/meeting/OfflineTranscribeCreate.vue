<script setup lang="ts">
import { computed, reactive, ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import type { Dayjs } from 'dayjs';
import { App, type Rule } from 'antdv-next';
const { message } = App.useApp();
import { useSpeakerStore } from '../../store/speaker';
import { hotWordApi } from '../../services/hotWordApi';
import type { HotWordLibraryDTO } from '../../services/hotWordApi';
import { meetingApi } from '../../services/meetingApi';
import {
  AudioOutlined,
  TeamOutlined,
  TagsOutlined,
  ArrowLeftOutlined,
  UserSwitchOutlined,
  FontSizeOutlined,
  ThunderboltOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  InboxOutlined,
} from '@antdv-next/icons';
import { useOfflineTranscribeStore } from '../../store/offlineTranscribe';

const router = useRouter();
const speakerStore = useSpeakerStore();
const offlineStore = useOfflineTranscribeStore();

const formRef = ref();
const formState = reactive({
  name: '',
  participants: '',
  meetingTime: [] as [Dayjs, Dayjs],
  speakers: [] as number[],
  hotWords: [] as number[],
});

const submitting = ref(false);
const mounted = ref(false);

const audioFile = computed(() => offlineStore.file);
const fileError = ref('');
const MAX_SIZE = 300 * 1024 * 1024; // 300MB

// 仅允许音频格式：驱动选择框过滤与上传校验，确保“只支持音频”
const AUDIO_EXTS = ['wav', 'mp3', 'm4a', 'aac', 'flac', 'ogg', 'opus', 'wma', 'aiff', 'caf', 'amr'];
const AUDIO_ACCEPT = `audio/*,${AUDIO_EXTS.map((e) => '.' + e).join(',')}`;
const AUDIO_EXT_RE = new RegExp(`\\.(${AUDIO_EXTS.join('|')})$`, 'i');
const isAudioFile = (f: File): boolean =>
  (f.type !== '' && f.type.startsWith('audio/')) || AUDIO_EXT_RE.test(f.name);

onMounted(() => {
  requestAnimationFrame(() => {
    mounted.value = true;
  });
  speakerStore.load().catch(() => {});
  loadLibraries();
});

// ---- 选项数据 ----
const libraries = ref<HotWordLibraryDTO[]>([]);
const hotWordLoading = ref(false);

async function loadLibraries() {
  hotWordLoading.value = true;
  try {
    const res = await hotWordApi.listLibraries({ pageSize: 1000 });
    libraries.value = res.items;
  } catch (err) {
    console.error('加载热词库失败:', err);
  } finally {
    hotWordLoading.value = false;
  }
}

const speakerOptions = computed(() =>
  speakerStore.list.map((s) => ({
    label: `${s.name} · ${s.language}`,
    value: s.id,
  })),
);

const hotWordOptions = computed(() =>
  libraries.value.map((lib) => ({
    label: `${lib.name}（${lib.word_count} 词）`,
    value: lib.id,
  })),
);

// ---- 表单验证 ----
const rules: Record<string, Rule[]> = {
  name: [
    { required: true, message: '请输入会议名称', whitespace: true },
    { max: 60, message: '会议名称不超过 60 个字符' },
  ],
  meetingTime: [
    {
      required: true,
      message: '请选择会议时间',
      validator: (_r, v: unknown) => {
        const range = v as [Dayjs, Dayjs] | [];
        if (!range || range.length !== 2 || !range[0] || !range[1]) {
          return Promise.reject(new Error('请选择会议开始与结束时间'));
        }
        return range[1].isAfter(range[0])
          ? Promise.resolve()
          : Promise.reject(new Error('结束时间须晚于开始时间'));
      },
    },
  ],
};

// ---- 全选/清空 ----
const allSpeakerIds = computed(() => speakerOptions.value.map((o) => o.value));
const allHotWordIds = computed(() => hotWordOptions.value.map((o) => o.value));

function toggleAll(key: 'speakers' | 'hotWords', all: number[]) {
  if (formState[key].length === all.length) formState[key] = [];
  else formState[key] = [...all];
}

// ---- 计算属性 ----
const previewName = computed(() => formState.name || '未命名会议');

const selectedParticipants = computed(() =>
  formState.participants
    .split(/[,，\n\s]+/)
    .map((p) => p.trim())
    .filter(Boolean),
);

const selectedSpeakers = computed(() =>
  speakerOptions.value.filter((o) => formState.speakers.includes(o.value)),
);

const selectedHotWords = computed(() =>
  hotWordOptions.value.filter((o) => formState.hotWords.includes(o.value)),
);

// 表单完成度 (0-6)：名称、会议时间、音频文件、参会人员、说话人、热词库
const completionCount = computed(() => {
  let c = 0;
  if (formState.name.trim()) c++;
  if (formState.meetingTime.length === 2) c++;
  if (audioFile.value) c++;
  if (selectedParticipants.value.length > 0) c++;
  if (formState.speakers.length > 0) c++;
  if (formState.hotWords.length > 0) c++;
  return c;
});

const completionPercent = computed(() => (completionCount.value / 6) * 100);

// 离线转写无需实时录音：会议名称、会议时间与音频文件均为必填
const isFormReady = computed(
  () => formState.name.trim().length > 0 && formState.meetingTime.length === 2 && !!audioFile.value,
);

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

// ---- 音频文件选择 ----
function beforeAudioUpload(file: unknown): false {
  const f = file as File;
  if (!isAudioFile(f)) {
    fileError.value = '请选择音频文件（wav / mp3 / m4a / aac / flac / ogg 等）';
    message.error(fileError.value);
    return false;
  }
  if (f.size > MAX_SIZE) {
    fileError.value = '音频文件过大，单个文件上限 300MB';
    message.error(fileError.value);
    return false;
  }
  fileError.value = '';
  offlineStore.setFile(f);
  message.success(`已选择音频：${f.name}`);
  return false; // 阻止自动上传，由转写页解码处理
}

function removeFile() {
  offlineStore.setFile(null);
}

// 仅在 before-upload 返回 false 时拦截，这里提供 no-op 以满足 antdv 的上传要求
function noopUpload() {
  /* no-op */
}

// ---- 操作 ----
async function handleStart() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  if (!audioFile.value) {
    message.error('请先选择音频文件');
    return;
  }

  submitting.value = true;
  try {
    const payload: {
      name: string;
      participants: string;
      speaker_ids: string;
      hot_word_library_ids: string;
      start_time?: string;
      end_time?: string;
    } = {
      name: formState.name.trim(),
      participants: selectedParticipants.value.join('、'),
      speaker_ids: formState.speakers.join(','),
      hot_word_library_ids: formState.hotWords.join(','),
    };
    if (formState.meetingTime.length === 2) {
      payload.start_time = formState.meetingTime[0].format('YYYY-MM-DD HH:mm:ss');
      payload.end_time = formState.meetingTime[1].format('YYYY-MM-DD HH:mm:ss');
    }

    const meeting = await meetingApi.createMeeting(payload);

    router.push({
      name: 'offlineTranscribe',
      query: {
        meetingId: String(meeting.id),
        name: formState.name,
        participants: selectedParticipants.value.join(','),
        speakers: formState.speakers.join(','),
        hotWords: formState.hotWords.join(','),
        startTime: payload.start_time,
        endTime: payload.end_time,
      },
    });
  } catch (err) {
    message.error((err as Error)?.message || '创建会议失败，请稍后重试');
    submitting.value = false;
  }
}

function handleBack() {
  offlineStore.clear();
  router.push({ name: 'home' });
}
</script>

<template>
  <div class="offline-create" :class="{ 'is-mounted': mounted }">
    <div class="offline-create__inner">
      <!-- 顶部导航 -->
      <header class="page-header" :style="{ '--i': 0 }">
        <a-button type="text" class="back-btn" @click="handleBack">
          <template #icon><ArrowLeftOutlined /></template>
          返回首页
        </a-button>
        <div class="title-row">
          <div class="title-icon">
            <AudioOutlined />
          </div>
          <div class="title-text">
            <h1>创建音频转写</h1>
            <p>上传音频文件，离线转写为文字稿并自动识别说话人与热词</p>
          </div>
        </div>
      </header>

      <!-- 主内容区：表单 + 预览 -->
      <div class="main-content">
        <!-- 表单卡片 -->
        <section class="form-section" :style="{ '--i': 1 }">
          <a-card class="form-card" variant="borderless">
            <a-form
              ref="formRef"
              :model="formState"
              :rules="rules"
              layout="vertical"
              class="create-form"
            >
              <!-- ===== 第一部分：会议信息 ===== -->
              <div class="form-section-header">
                <span class="section-badge">会议信息</span>
                <span class="section-sub">设置会议基本信息与音频文件</span>
              </div>

              <a-form-item label="会议名称" name="name">
                <a-input
                  v-model:value="formState.name"
                  placeholder="例如：Q3 产品评审会 / 客户需求访谈"
                  allow-clear
                  :maxlength="60"
                  show-count
                >
                  <template #prefix><AudioOutlined /></template>
                </a-input>
              </a-form-item>

              <a-form-item name="participants" label="参会人员（选填）">
                <a-input
                  v-model:value="formState.participants"
                  placeholder="输入参会人员姓名，多个用逗号或换行分隔"
                  allow-clear
                >
                  <template #prefix><TeamOutlined /></template>
                </a-input>
              </a-form-item>

              <a-form-item name="meetingTime" label="会议时间">
                <a-range-picker
                  v-model:value="formState.meetingTime"
                  show-time
                  format="YYYY-MM-DD HH:mm"
                  placeholder="['开始时间', '结束时间']"
                  style="width: 100%"
                >
                  <template #suffixIcon><ClockCircleOutlined /></template>
                </a-range-picker>
              </a-form-item>

              <!-- 离线转写核心差异：用户主动添加音频文件，而非实时录音 -->
              <a-form-item label="音频文件" required>
                <a-upload-dragger
                  class="audio-drop"
                  :before-upload="beforeAudioUpload"
                  :custom-request="noopUpload"
                  :show-upload-list="false"
                  :accept="AUDIO_ACCEPT"
                >
                  <div v-if="!audioFile" class="audio-drop__inner">
                    <p class="audio-drop__icon"><InboxOutlined /></p>
                    <p class="audio-drop__title">点击或拖拽音频文件到此处</p>
                    <p class="audio-drop__hint">支持 wav / mp3 / m4a / aac / flac / ogg 等格式，单文件 ≤ 300MB</p>
                  </div>
                </a-upload-dragger>

                <div v-if="audioFile" class="audio-info">
                  <AudioOutlined class="audio-info__icon" />
                  <div class="audio-info__meta">
                    <div class="audio-info__name" :title="audioFile.name">{{ audioFile.name }}</div>
                    <div class="audio-info__sub">{{ formatSize(audioFile.size) }} · 已就绪，将离线转写</div>
                  </div>
                  <a-button type="text" danger class="audio-info__remove" @click="removeFile">
                    <template #icon><DeleteOutlined /></template>
                    移除
                  </a-button>
                </div>

                <p v-if="fileError" class="file-error-tip">{{ fileError }}</p>
                <p class="field-hint">
                  离线转写无需实时录音，上传录制好的音频文件即可在转写页自动生成文字稿
                </p>
              </a-form-item>

              <!-- ===== 分隔线 ===== -->
              <div class="section-divider" :style="{ '--i': 2 }">
                <span class="divider-label">转写配置</span>
              </div>

              <!-- ===== 第二部分：转写配置 ===== -->
              <div class="form-section-header" style="margin-top: 0">
                <span class="section-badge section-badge--secondary">转写增强</span>
                <span class="section-sub">配置说话人识别与热词库，提升转写准确率</span>
              </div>

              <a-form-item name="speakers">
                <template #label>
                  <span class="field-label-row">
                    <span class="field-label">
                      <UserSwitchOutlined /> 说话人（选填）
                    </span>
                    <a
                      class="field-toggle"
                      @click="toggleAll('speakers', allSpeakerIds)"
                    >
                      {{ formState.speakers.length === allSpeakerIds.length ? '清空' : '全选' }}
                    </a>
                  </span>
                </template>
                <a-select
                  v-model:value="formState.speakers"
                  mode="multiple"
                  placeholder="选择参与转写的说话人"
                  :options="speakerOptions"
                  allow-clear
                  show-search
                  option-filter-prop="label"
                  max-tag-count="responsive"
                />
              </a-form-item>

              <a-form-item name="hotWords">
                <template #label>
                  <span class="field-label-row">
                    <span class="field-label">
                      <FontSizeOutlined /> 热词库（选填）
                    </span>
                    <a
                      class="field-toggle"
                      @click="toggleAll('hotWords', allHotWordIds)"
                    >
                      {{ formState.hotWords.length === allHotWordIds.length ? '清空' : '全选' }}
                    </a>
                  </span>
                </template>
                <a-select
                  v-model:value="formState.hotWords"
                  mode="multiple"
                  placeholder="选择热词库，提升专业术语识别率"
                  :options="hotWordOptions"
                  :loading="hotWordLoading"
                  allow-clear
                  show-search
                  option-filter-prop="label"
                  max-tag-count="responsive"
                >
                  <template #notFoundContent>
                    <a-empty description="暂无热词库" :image="false" />
                  </template>
                  <template #suffixIcon><TagsOutlined /></template>
                </a-select>
              </a-form-item>

              <!-- ===== 提交按钮 ===== -->
              <a-form-item class="submit-area">
                <a-button
                  type="primary"
                  size="large"
                  block
                  :loading="submitting"
                  :disabled="!isFormReady"
                  @click="handleStart"
                  class="start-btn"
                >
                  <template #icon><ThunderboltOutlined /></template>
                  开始离线转写
                </a-button>
                <p v-if="!isFormReady" class="submit-hint">
                  请先填写会议名称、会议时间并上传音频文件（其余信息均为选填）
                </p>
              </a-form-item>
            </a-form>
          </a-card>
        </section>

        <!-- 预览面板 -->
        <aside class="preview-section" :style="{ '--i': 2 }">
          <div class="preview-card" :class="{ 'is-active': completionCount > 0 }">
            <div class="preview-header">
              <div class="preview-pulse" :class="{ active: isFormReady }">
                <span class="pulse-dot" />
              </div>
              <span class="preview-title">会议预览</span>
            </div>

            <!-- 完成度进度条 -->
            <div class="preview-progress">
              <div
                class="preview-progress-bar"
                :style="{ width: completionPercent + '%' }"
              />
            </div>

            <!-- 会议名称 -->
            <div class="preview-field">
              <span class="preview-label">会议名称</span>
              <span class="preview-value" :class="{ placeholder: !formState.name }">
                {{ previewName }}
              </span>
            </div>

            <!-- 参会人数 -->
            <div class="preview-field">
              <span class="preview-label">参会人员</span>
              <span class="preview-value">
                <template v-if="selectedParticipants.length">
                  {{ selectedParticipants.length }} 人
                </template>
                <span v-else class="placeholder">—</span>
              </span>
            </div>

            <!-- 会议时间 -->
            <div class="preview-field">
              <span class="preview-label">会议时间</span>
              <span class="preview-value">
                <template v-if="formState.meetingTime.length === 2">
                  {{ formState.meetingTime[0].format('YYYY-MM-DD HH:mm') }}
                  <br />
                  {{ formState.meetingTime[1].format('YYYY-MM-DD HH:mm') }}
                </template>
                <span v-else class="placeholder">—</span>
              </span>
            </div>

            <div v-if="selectedParticipants.length" class="preview-tags">
              <a-tag
                v-for="p in selectedParticipants.slice(0, 5)"
                :key="p"
                color="blue"
                class="preview-tag"
              >
                {{ p }}
              </a-tag>
              <a-tag v-if="selectedParticipants.length > 5" class="preview-tag">
                +{{ selectedParticipants.length - 5 }}
              </a-tag>
            </div>

            <!-- 音频文件 -->
            <div class="preview-field">
              <span class="preview-label">音频文件</span>
              <span class="preview-value">
                <template v-if="audioFile">
                  <span class="preview-file-name" :title="audioFile.name">{{ audioFile.name }}</span>
                  <span class="preview-file-size">{{ formatSize(audioFile.size) }}</span>
                </template>
                <span v-else class="placeholder">—</span>
              </span>
            </div>

            <!-- 说话人 -->
            <div class="preview-field">
              <span class="preview-label">说话人</span>
              <span class="preview-value">
                <template v-if="formState.speakers.length">
                  {{ formState.speakers.length }} 人
                </template>
                <span v-else class="placeholder">—</span>
              </span>
            </div>
            <div v-if="selectedSpeakers.length" class="preview-tags">
              <a-tag
                v-for="s in selectedSpeakers.slice(0, 3)"
                :key="s.value"
                color="purple"
                class="preview-tag"
              >
                {{ s.label }}
              </a-tag>
              <a-tag v-if="selectedSpeakers.length > 3" class="preview-tag">
                +{{ selectedSpeakers.length - 3 }}
              </a-tag>
            </div>

            <!-- 热词库 -->
            <div class="preview-field">
              <span class="preview-label">热词库</span>
              <span class="preview-value">
                <template v-if="formState.hotWords.length">
                  {{ formState.hotWords.length }} 个
                </template>
                <span v-else class="placeholder">—</span>
              </span>
            </div>
            <div v-if="selectedHotWords.length" class="preview-tags">
              <a-tag
                v-for="h in selectedHotWords.slice(0, 3)"
                :key="h.value"
                color="green"
                class="preview-tag"
              >
                {{ h.label }}
              </a-tag>
              <a-tag v-if="selectedHotWords.length > 3" class="preview-tag">
                +{{ selectedHotWords.length - 3 }}
              </a-tag>
            </div>

            <!-- 空状态 -->
            <div v-if="completionCount === 0" class="preview-empty">
              <span>开始填写表单并上传音频文件，这里将实时显示会议配置</span>
            </div>
          </div>
        </aside>
      </div>
    </div>
  </div>
</template>

<style scoped>
.offline-create {
  --radius-lg: 18px;
  --radius-md: 12px;
  --radius-sm: 8px;
  --shadow-card: var(--shadow-sm);
  --shadow-glow: 0 0 40px rgba(22, 119, 255, 0.15);
  --transition-base: 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

.offline-create {
  position: relative;
  min-height: 100vh;
  box-sizing: border-box;
  padding: 28px 24px 56px;
  color: var(--color-text);
  background: radial-gradient(
      1200px 600px at 80% -10%,
      var(--color-brand-soft),
      transparent 60%
    ),
    var(--color-bg);
}

.offline-create__inner {
  position: relative;
  z-index: 1;
  max-width: 1200px;
  margin: 0 auto;
}

.page-header {
  opacity: 0;
  transform: translateY(12px);
  transition: opacity 0.6s ease, transform 0.6s ease;
  transition-delay: calc(var(--i, 0) * 0.1s);
}
.is-mounted .page-header {
  opacity: 1;
  transform: translateY(0);
}

.back-btn {
  color: var(--color-text-secondary);
  margin-left: -8px;
  margin-bottom: 16px;
  font-size: 13px;
  transition: color var(--transition-base);
}
.back-btn:hover {
  color: var(--color-brand-hover) !important;
}

.title-row {
  display: flex;
  align-items: center;
  gap: 18px;
  margin-bottom: 28px;
}
.title-icon {
  width: 52px;
  height: 52px;
  border-radius: var(--radius-md);
  background: linear-gradient(135deg, #52c41a 0%, #73d13d 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  color: #fff;
  box-shadow: 0 4px 20px rgba(82, 196, 26, 0.3);
  flex-shrink: 0;
}
.title-text h1 {
  margin: 0;
  color: var(--color-text);
  font-size: 24px;
  font-weight: 700;
  letter-spacing: -0.02em;
  line-height: 1.3;
}
.title-text p {
  margin: 4px 0 0;
  color: var(--color-text-secondary);
  font-size: 14px;
}

.main-content {
  display: grid;
  grid-template-columns: 1fr 340px;
  gap: 24px;
  align-items: start;
}

.form-section {
  opacity: 0;
  transform: translateY(16px);
  transition: opacity 0.6s ease, transform 0.6s ease;
  transition-delay: calc(var(--i, 0) * 0.1s + 0.15s);
}
.preview-section {
  opacity: 0;
  transform: translateY(16px);
  transition: opacity 0.6s ease, transform 0.6s ease;
  transition-delay: calc(var(--i, 0) * 0.1s + 0.25s);
}
.is-mounted .form-section,
.is-mounted .preview-section {
  opacity: 1;
  transform: translateY(0);
}

.form-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}
.form-card :deep(.ant-card-body) {
  padding: 32px 36px 36px;
}

.form-section-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 24px;
}
.section-badge {
  display: inline-flex;
  align-items: center;
  height: 26px;
  padding: 0 10px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
  background: var(--color-brand-soft);
  color: var(--color-brand);
  white-space: nowrap;
}
.section-badge--secondary {
  background: var(--color-brand-soft);
  color: var(--color-brand-hover);
}
.section-sub {
  font-size: 12px;
  color: var(--color-text-muted);
}

.section-divider {
  display: flex;
  align-items: center;
  gap: 16px;
  margin: 8px 0 28px;
}
.section-divider::before,
.section-divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--color-border);
}
.divider-label {
  font-size: 11px;
  font-weight: 500;
  color: var(--color-text-muted);
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.create-form :deep(.ant-form-item) {
  margin-bottom: 22px;
}
.create-form :deep(.ant-form-item-label > label) {
  color: var(--color-text-secondary);
  font-size: 13px;
  font-weight: 500;
  height: auto;
  line-height: 1.5;
}
.create-form :deep(.ant-form-item-explain-error) {
  font-size: 12px;
  color: var(--color-error, #ff4d4f);
}

.create-form :deep(.ant-input),
.create-form :deep(.ant-select-selector) {
  background: var(--color-surface) !important;
  border-color: var(--color-border) !important;
  color: var(--color-text) !important;
  border-radius: var(--radius-sm) !important;
  transition: all var(--transition-base);
}
.create-form :deep(.ant-input:hover),
.create-form :deep(.ant-select:hover .ant-select-selector) {
  border-color: var(--color-border-strong) !important;
}
.create-form :deep(.ant-input:focus),
.create-form :deep(.ant-select-focused .ant-select-selector) {
  border-color: var(--color-brand) !important;
  box-shadow: 0 0 0 2px var(--color-brand-soft) !important;
  background: var(--color-surface) !important;
}
.create-form :deep(.ant-input::placeholder),
.create-form :deep(.ant-select-selection-placeholder) {
  color: var(--color-text-quaternary, rgba(0, 0, 0, 0.25));
}
.create-form :deep(.ant-input-prefix) {
  color: var(--color-text-muted);
  margin-right: 8px;
}
.create-form :deep(.ant-select-arrow) {
  color: var(--color-text-muted);
}
.create-form :deep(.ant-tag) {
  background: var(--color-brand-soft);
  border-color: var(--color-border);
  color: var(--color-brand);
  border-radius: 4px;
}
.create-form :deep(.ant-select-clear) {
  color: var(--color-text-muted);
}

.field-label-row {
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}
.field-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--color-text) !important;
}
.field-toggle {
  font-size: 12px;
  color: var(--color-brand-hover);
  font-weight: 400;
  cursor: pointer;
  transition: color var(--transition-base);
  user-select: none;
}
.field-toggle:hover {
  color: var(--color-brand);
}

.field-hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--color-text-muted);
  line-height: 1.5;
}

.file-error-tip {
  margin: 8px 0 0;
  font-size: 12px;
  color: #ff4d4f;
  line-height: 1.5;
}

/* 音频上传区 */
.audio-drop {
  width: 100%;
}
.audio-drop :deep(.ant-upload-drag) {
  border-color: var(--color-border-strong) !important;
  border-radius: var(--radius-md) !important;
  background: var(--color-surface-2, #fafafa) !important;
  transition: all var(--transition-base);
}
.audio-drop :deep(.ant-upload-drag):hover {
  border-color: var(--color-brand) !important;
}
.audio-drop__inner {
  padding: 28px 16px;
  text-align: center;
}
.audio-drop__icon {
  font-size: 32px;
  color: var(--color-success, #52c41a);
  margin: 0 0 8px;
}
.audio-drop__title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
}
.audio-drop__hint {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--color-text-muted);
}

.audio-info {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 12px;
  padding: 14px 16px;
  background: var(--color-surface-2, #fafafa);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
}
.audio-info__icon {
  font-size: 22px;
  color: var(--color-success, #52c41a);
  flex-shrink: 0;
}
.audio-info__meta {
  flex: 1;
  min-width: 0;
}
.audio-info__name {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.audio-info__sub {
  margin-top: 2px;
  font-size: 12px;
  color: var(--color-text-muted);
}
.audio-info__remove {
  flex-shrink: 0;
}

.submit-area {
  margin-top: 32px;
  margin-bottom: 0 !important;
}
.submit-area :deep(.ant-form-item-control-input-content) {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.start-btn {
  height: 48px !important;
  font-size: 15px !important;
  font-weight: 600 !important;
  border-radius: var(--radius-md) !important;
  background: linear-gradient(135deg, #52c41a 0%, #73d13d 100%) !important;
  border: none !important;
  box-shadow: 0 4px 20px rgba(82, 196, 26, 0.3);
  transition: all var(--transition-base);
  letter-spacing: 0.02em;
}
.start-btn:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 6px 28px rgba(82, 196, 26, 0.45);
}
.start-btn:active:not(:disabled) {
  transform: translateY(0);
}
.start-btn:disabled {
  background: var(--color-surface-2, #fafafa) !important;
  box-shadow: none;
  color: var(--color-text-quaternary, rgba(0, 0, 0, 0.25)) !important;
  cursor: not-allowed;
}

.submit-hint {
  margin: 12px 0 0;
  font-size: 12px;
  color: var(--color-text-muted);
  text-align: center;
}

.preview-section {
  position: sticky;
  top: 28px;
}

.preview-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  padding: 24px;
  transition: all var(--transition-base);
  box-shadow: var(--shadow-card);
}
.preview-card.is-active {
  border-color: var(--color-success, #52c41a);
  box-shadow: var(--shadow-card), 0 0 40px rgba(82, 196, 26, 0.15);
}

.preview-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}
.preview-pulse {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: var(--color-surface-2, #fafafa);
  border: 1px solid var(--color-border);
  transition: all var(--transition-base);
}
.preview-pulse.active {
  border-color: var(--color-success, #52c41a);
}
.pulse-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-text-quaternary, rgba(0, 0, 0, 0.25));
  transition: all var(--transition-base);
}
.preview-pulse.active .pulse-dot {
  background: var(--color-success, #52c41a);
  box-shadow: 0 0 10px rgba(82, 196, 26, 0.6);
  animation: pulse-dot 2s ease-in-out infinite;
}
@keyframes pulse-dot {
  0%, 100% { box-shadow: 0 0 6px rgba(82, 196, 26, 0.4); }
  50% { box-shadow: 0 0 16px rgba(82, 196, 26, 0.8); }
}

.preview-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text-secondary);
  letter-spacing: 0.02em;
}

.preview-progress {
  height: 3px;
  border-radius: 2px;
  background: var(--color-border-secondary, #f0f0f0);
  margin-bottom: 20px;
  overflow: hidden;
}
.preview-progress-bar {
  height: 100%;
  border-radius: 2px;
  background: linear-gradient(90deg, #52c41a, #73d13d);
  transition: width 0.5s cubic-bezier(0.4, 0, 0.2, 1);
}

.preview-field {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid var(--color-border-secondary, #f0f0f0);
}
.preview-field:last-of-type {
  border-bottom: none;
}
.preview-label {
  font-size: 12px;
  color: var(--color-text-muted);
  flex-shrink: 0;
}
.preview-value {
  font-size: 13px;
  color: var(--color-text);
  text-align: right;
  word-break: break-all;
  max-width: 210px;
}
.preview-value.placeholder {
  color: var(--color-text-muted);
}
.placeholder {
  color: var(--color-text-muted);
}
.preview-file-name {
  display: block;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.preview-file-size {
  font-size: 11px;
  color: var(--color-text-muted);
}

.preview-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: -4px;
  margin-bottom: 8px;
  padding-left: 60px;
}
.preview-tag {
  font-size: 11px !important;
  line-height: 20px !important;
  background: var(--color-surface-2, #fafafa) !important;
  border-color: var(--color-border) !important;
  color: var(--color-text-secondary) !important;
  border-radius: 4px !important;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.preview-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 16px;
  text-align: center;
}
.preview-empty span {
  font-size: 12px;
  color: var(--color-text-muted);
  line-height: 1.6;
}

@media (max-width: 900px) {
  .main-content {
    grid-template-columns: 1fr;
  }
  .preview-section {
    position: static;
  }
}
</style>
