<script setup lang="ts">
import { computed, reactive, ref, onMounted, onBeforeUnmount, watch } from 'vue';
import { useRouter } from 'vue-router';
import type { Rule } from 'antdv-next';
import { useSystemUserStore } from '../../store/systemUser';
import { useSpeakerStore } from '../../store/speaker';
import { useHotWordStore } from '../../store/hotWord';
import {
  VideoCameraOutlined,
  AudioOutlined,
  SoundOutlined,
  TeamOutlined,
  TagsOutlined,
  ArrowLeftOutlined,
  UserSwitchOutlined,
  FontSizeOutlined,
  ThunderboltOutlined,
} from '@antdv-next/icons';
import { createSystemAudioStream } from '../../services/capture';
import audioProcessorCode from '@/worklets/audio-processor.js?raw';

const router = useRouter();
const userStore = useSystemUserStore();
const speakerStore = useSpeakerStore();
const hotWordStore = useHotWordStore();

const formRef = ref();
const formState = reactive({
  name: '',
  participants: [] as number[],
  recordMode: 'mic' as 'mic' | 'system',
  speakers: [] as number[],
  hotWords: [] as number[],
});

const submitting = ref(false);
const mounted = ref(false);

// ==========================================
// 音频采集状态
// ==========================================
/** 当前采集类型 */
const captureType = ref<'none' | 'mic' | 'system'>('none');
/** 系统内录错误信息 */
const systemError = ref('');
const isListening = ref(false);
const currentVolume = ref(0); // 0-1 归一化音量

let audioContext: AudioContext | null = null;
let activeStream: MediaStream | null = null;
let placeholderVideoTracks: MediaStreamTrack[] = [];
let sourceNode: MediaStreamAudioSourceNode | null = null;
let systemCaptureStop: (() => void) | null = null;
let workletNode: AudioWorkletNode | null = null;
let workletAdded = false;
let animationId = 0;
let latestRms = 0;

onMounted(() => {
  requestAnimationFrame(() => {
    mounted.value = true;
  });
  // 默认麦克风模式，自动开启录音音量展示
  if (formState.recordMode === 'mic') {
    startMic();
  }
});

// ---- 选项数据 ----
const participantOptions = userStore.list
  .filter((u) => u.status === '启用')
  .map((u) => ({ label: `${u.name}（${u.username}）`, value: u.id }));

const speakerOptions = speakerStore.list.map((s) => ({
  label: `${s.name} · ${s.language}`,
  value: s.id,
}));

const hotWordOptions = hotWordStore.list.map((w) => ({
  label: `${w.word} · ${w.category}`,
  value: w.id,
}));

// ---- 表单验证 ----
const rules: Record<string, Rule[]> = {
  name: [{ required: true, message: '请输入会议名称', whitespace: true }],
  participants: [
    {
      validator: (_r, v) =>
        Array.isArray(v) && v.length > 0
          ? Promise.resolve()
          : Promise.reject(new Error('请至少选择一位参会人员')),
    },
  ],
  speakers: [
    {
      validator: (_r, v) =>
        Array.isArray(v) && v.length > 0
          ? Promise.resolve()
          : Promise.reject(new Error('请至少选择一位说话人')),
    },
  ],
  hotWords: [
    {
      validator: (_r, v) =>
        Array.isArray(v) && v.length > 0
          ? Promise.resolve()
          : Promise.reject(new Error('请至少选择一个热词库')),
    },
  ],
};

// ---- 全选/清空 ----
const allParticipantIds = participantOptions.map((o) => o.value);
const allSpeakerIds = speakerOptions.map((o) => o.value);
const allHotWordIds = hotWordOptions.map((o) => o.value);

function toggleAll(key: 'participants' | 'speakers' | 'hotWords', all: number[]) {
  if (formState[key].length === all.length) formState[key] = [];
  else formState[key] = [...all];
}

// ---- 计算属性 ----
// 预览面板数据
const previewName = computed(() => formState.name || '未命名会议');

const selectedParticipants = computed(() =>
  participantOptions.filter((o) => formState.participants.includes(o.value)),
);

const selectedSpeakers = computed(() =>
  speakerOptions.filter((o) => formState.speakers.includes(o.value)),
);

const selectedHotWords = computed(() =>
  hotWordOptions.filter((o) => formState.hotWords.includes(o.value)),
);

// 表单完成度 (0-5)
const completionCount = computed(() => {
  let c = 0;
  if (formState.name.trim()) c++;
  if (formState.participants.length > 0) c++;
  if (formState.recordMode) c++;
  if (formState.speakers.length > 0) c++;
  if (formState.hotWords.length > 0) c++;
  return c;
});

const completionPercent = computed(() => (completionCount.value / 5) * 100);

const isFormReady = computed(() => completionCount.value >= 3);

// ---- 操作 ----
async function handleStart() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  submitting.value = true;
  router.push({
    name: 'liveTranscribe',
    query: {
      name: formState.name,
      participants: formState.participants.join(','),
      recordMode: formState.recordMode,
      speakers: formState.speakers.join(','),
      hotWords: formState.hotWords.join(','),
    },
  });
}

function handleBack() {
  router.push({ name: 'home' });
}

// ==========================================
// 音频采集：麦克风 / 系统内录
// ==========================================

/** 懒初始化 AudioContext，并注册 audio-processor AudioWorklet 模块 */
async function ensureAudioGraph() {
  if (!audioContext) {
    audioContext = new (window.AudioContext || (window as any).webkitAudioContext)();
    workletAdded = false;
  }
  if (audioContext.state === 'suspended') {
    await audioContext.resume();
  }
  if (!workletAdded) {
    const blob = new Blob([audioProcessorCode], { type: 'application/javascript' });
    const url = URL.createObjectURL(blob);
    try {
      await audioContext.audioWorklet.addModule(url);
      workletAdded = true;
    } finally {
      URL.revokeObjectURL(url);
    }
  }
}

/** 建立 audio-processor AudioWorklet 采集节点（source → worklet → destination），由 worklet 输出的 PCM 计算实时音量 */
function attachRecordingWorklet() {
  if (!audioContext || !sourceNode) return;
  const node = new AudioWorkletNode(audioContext, 'audio-processor');
  // worklet 每帧回传 16bit PCM（ArrayBuffer），直接在此计算 RMS 驱动音量条
  node.port.onmessage = (e: MessageEvent) => {
    const samples = new Int16Array(e.data as ArrayBuffer);
    let sum = 0;
    for (let i = 0; i < samples.length; i++) {
      const v = samples[i] / 32768;
      sum += v * v;
    }
    latestRms = samples.length ? Math.sqrt(sum / samples.length) : 0;
  };
  sourceNode.connect(node);
  node.connect(audioContext.destination);
  workletNode = node;
  startVolumeLoop();
}

/** 麦克风采集：getUserMedia 直接获取 */
async function startMic() {
  try {
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
      },
    });
    await ensureAudioGraph();
    activeStream = stream;
    placeholderVideoTracks = [];
    sourceNode = audioContext!.createMediaStreamSource(stream);
    attachRecordingWorklet();

    captureType.value = 'mic';
    systemError.value = '';
    isListening.value = true;
    startVolumeLoop();
  } catch (err: any) {
    console.error('Microphone access error:', err);
  }
}

/**
 * 系统内录：经由主进程 setDisplayMediaRequestHandler 提供的 loopback 音频轨采集系统声音。
 * 渲染进程先通过 captureApi 写入采集配置（audio: 'system'），再调用 getDisplayMedia，
 * 主进程会拦截并返回带系统音频的流；占位视频轨在 createSystemAudioStream 内部已剔除并在 stop 时释放。
 */
async function startSystem() {
  try {
    // 动态加载：capture 服务依赖 electron IPC，仅在 Electron 运行时可用，
    // 惰性加载可避免非 Electron 环境下顶层 import 导致整页崩溃
    const sys = await createSystemAudioStream({ silent: false });
    systemCaptureStop = sys.stop;

    await ensureAudioGraph();
    activeStream = sys.stream;
    placeholderVideoTracks = [];
    sourceNode = audioContext!.createMediaStreamSource(activeStream);
    attachRecordingWorklet();

    // 用户主动停止屏幕共享时同步收尾
    activeStream.getAudioTracks()[0].onended = () => stopCapture();

    captureType.value = 'system';
    systemError.value = '';
    isListening.value = true;
    startVolumeLoop();
  } catch (err: any) {
    systemCaptureStop = null;
    systemError.value = err?.message || '系统内录失败';
    console.error('System audio capture error:', err);
  }
}

/** 停止采集（两种模式通用） */
function stopCapture() {
  cancelAnimationFrame(animationId);
  if (sourceNode) {
    sourceNode.disconnect();
    sourceNode = null;
  }
  if (workletNode) {
    try {
      workletNode.disconnect();
    } catch {
      /* noop */
    }
    try {
      workletNode.port.close();
    } catch {
      /* noop */
    }
    workletNode = null;
  }
  if (activeStream) {
    activeStream.getTracks().forEach((t) => {
      t.onended = null;
      t.stop();
    });
    activeStream = null;
  }
  placeholderVideoTracks.forEach((t) => t.stop());
  placeholderVideoTracks = [];
  if (systemCaptureStop) {
    systemCaptureStop();
    systemCaptureStop = null;
  }
  if (audioContext && audioContext.state !== 'closed') {
    audioContext.close().catch(() => {});
    audioContext = null;
  }
  isListening.value = false;
  captureType.value = 'none';
  currentVolume.value = 0;
}

/** 音量采集循环：基于 worklet 计算的 RMS 平滑出实时音量（仅用 audio-processor，无 AnalyserNode） */
function startVolumeLoop() {
  cancelAnimationFrame(animationId);
  const update = () => {
    if (!isListening.value) return;
    // 幂函数曲线：把低音量区间拉开，更接近听觉感知
    const expanded = Math.min(Math.pow(latestRms, 0.28), 1);
    currentVolume.value = currentVolume.value * 0.3 + expanded * 0.7;
    animationId = requestAnimationFrame(update);
  };
  update();
}

/** 监听录音模式切换 */
watch(
  () => formState.recordMode,
  (mode) => {
    if (mode === 'mic') {
      if (captureType.value !== 'mic') {
        stopCapture();
        startMic();
      }
    } else {
      // 切换为系统内录：由 radio 切换触发用户手势，直接自动开启内录
      stopCapture();
      startSystem();
    }
  },
);

onBeforeUnmount(() => {
  stopCapture();
});
</script>

<template>
  <div class="live-create" :class="{ 'is-mounted': mounted }">
    <div class="live-create__inner">
      <!-- 顶部导航 -->
      <header class="page-header" :style="{ '--i': 0 }">
        <a-button type="text" class="back-btn" @click="handleBack">
          <template #icon><ArrowLeftOutlined /></template>
          返回首页
        </a-button>
        <div class="title-row">
          <div class="title-icon">
            <VideoCameraOutlined />
          </div>
          <div class="title-text">
            <h1>创建实时会议</h1>
            <p>配置会议信息，一键开启实时转写与字幕</p>
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
                <span class="section-sub">设置会议基本信息与录音方式</span>
              </div>

              <a-form-item label="会议名称" name="name">
                <a-input
                  v-model:value="formState.name"
                  placeholder="例如：Q3 产品评审会 / 客户需求访谈"
                  allow-clear
                  :maxlength="60"
                  show-count
                >
                  <template #prefix><VideoCameraOutlined /></template>
                </a-input>
              </a-form-item>

              <a-form-item name="participants">
                <template #label>
                  <span class="field-label-row">
                    <span class="field-label">
                      <TeamOutlined /> 参会人员
                    </span>
                    <a
                      class="field-toggle"
                      @click="toggleAll('participants', allParticipantIds)"
                    >
                      {{ formState.participants.length === allParticipantIds.length ? '清空' : '全选' }}
                    </a>
                  </span>
                </template>
                <a-select
                  v-model:value="formState.participants"
                  mode="multiple"
                  placeholder="搜索并选择参会人员"
                  :options="participantOptions"
                  allow-clear
                  show-search
                  option-filter-prop="label"
                  max-tag-count="responsive"
                >
                  <template #notFoundContent>
                    <a-empty description="没有可用的参会人员" :image="false" />
                  </template>
                </a-select>
              </a-form-item>

              <a-form-item label="录音方式" name="recordMode">
                <div class="record-mode-row">
                  <a-radio-group v-model:value="formState.recordMode" class="record-radio-group">
                    <a-radio-button value="mic">
                      <AudioOutlined />
                      <span class="radio-label-text">麦克风录音</span>
                    </a-radio-button>
                    <a-radio-button value="system">
                      <SoundOutlined />
                      <span class="radio-label-text">系统内录</span>
                    </a-radio-button>
                  </a-radio-group>

                  <!-- 录音状态指示 -->
                  <div class="inline-volume">
                    <span class="inline-volume-label">录音状态</span>

                    <!-- 麦克风模式：自动采集，直接显示音量条 -->
                    <template v-if="formState.recordMode === 'mic'">
                      <div class="volume-meter">
                        <div
                          class="volume-meter-fill"
                          :style="{
                            width: (currentVolume * 100) + '%',
                            background: `linear-gradient(90deg, hsl(${210 - currentVolume * 210}, 85%, 55%), hsl(${210 - currentVolume * 210}, 85%, 65%))`,
                          }"
                        ></div>
                      </div>
                    </template>

                    <!-- 系统内录 -->
                    <template v-else>
                      <div class="volume-meter">
                        <div
                          class="volume-meter-fill"
                          :style="{
                            width: (currentVolume * 100) + '%',
                            background: `linear-gradient(90deg, hsl(${210 - currentVolume * 210}, 85%, 55%), hsl(${210 - currentVolume * 210}, 85%, 65%))`,
                          }"
                        ></div>
                      </div>
                      <span v-if="systemError" class="sys-error-tip">{{ systemError }}</span>
                    </template>
                  </div>
                </div>

                <p class="field-hint">
                  {{ formState.recordMode === 'mic' ? '通过设备麦克风采集现场声音，适合线下会议、访谈等场景' : '采集系统播放的音频（如线上会议、视频通话），适合远程会议' }}
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
                      <UserSwitchOutlined /> 说话人
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
                      <FontSizeOutlined /> 热词库
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
                  allow-clear
                  show-search
                  option-filter-prop="label"
                  max-tag-count="responsive"
                >
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
                  开始实时转写
                </a-button>
                <p v-if="!isFormReady" class="submit-hint">
                  请至少填写会议名称、选择参会人员和录音方式
                </p>
              </a-form-item>
            </a-form>
          </a-card>
        </section>

        <!-- 预览面板 -->
        <aside class="preview-section" :style="{ '--i': 2 }">
          <div class="preview-card" :class="{ 'is-active': completionCount > 0 }">
            <div class="preview-header">
              <div class="preview-pulse" :class="{ active: completionCount >= 3 }">
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
                <template v-if="formState.participants.length">
                  {{ formState.participants.length }} 人
                </template>
                <span v-else class="placeholder">—</span>
              </span>
            </div>
            <div v-if="selectedParticipants.length" class="preview-tags">
              <a-tag
                v-for="p in selectedParticipants.slice(0, 5)"
                :key="p.value"
                color="blue"
                class="preview-tag"
              >
                {{ p.label }}
              </a-tag>
              <a-tag v-if="selectedParticipants.length > 5" class="preview-tag">
                +{{ selectedParticipants.length - 5 }}
              </a-tag>
            </div>

            <!-- 录音方式 -->
            <div class="preview-field">
              <span class="preview-label">录音方式</span>
              <span class="preview-value">
                <template v-if="formState.recordMode === 'mic'">
                  <AudioOutlined style="margin-right: 4px" /> 麦克风录音
                </template>
                <template v-else>
                  <SoundOutlined style="margin-right: 4px" /> 系统内录
                </template>
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
              <span>开始填写表单，这里将实时显示会议配置</span>
            </div>
          </div>
        </aside>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ==========================================
   Design Tokens
   与首页 / admin 统一使用浅色商业主题（依赖全局主题变量）：
   --color-bg / --color-surface / --color-text / --color-border /
   --color-brand / --color-brand-soft / --color-brand-hover 等
   ========================================== */
.live-create {
  --radius-lg: 18px;
  --radius-md: 12px;
  --radius-sm: 8px;
  --shadow-card: var(--shadow-sm);
  --shadow-glow: 0 0 40px rgba(22, 119, 255, 0.15);
  --transition-base: 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}

/* ==========================================
   Layout & Background
   ========================================== */
.live-create {
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

/* ==========================================
   Inner Container
   ========================================== */
.live-create__inner {
  position: relative;
  z-index: 1;
  max-width: 1200px;
  margin: 0 auto;
}

/* ==========================================
   Page Header
   ========================================== */
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
  background: linear-gradient(135deg, #1677ff 0%, #4096ff 100%);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  color: #fff;
  box-shadow: 0 4px 20px rgba(22, 119, 255, 0.3);
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

/* ==========================================
   Main Content (Two Columns)
   ========================================== */
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

/* ==========================================
   Form Card
   ========================================== */
.form-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

.form-card :deep(.ant-card-body) {
  padding: 32px 36px 36px;
}

/* ==========================================
   Form Sections
   ========================================== */
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

/* 分隔线 */
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

/* ==========================================
   Form Fields
   ========================================== */
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

/* 输入框 */
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

/* 标签行 */
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

/* 字段提示文本 */
.field-hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--color-text-muted);
  line-height: 1.5;
}

/* 录音方式 radio-group */
.create-form :deep(.ant-radio-group) {
  display: flex;
}
.create-form :deep(.ant-radio-button-wrapper) {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  background: var(--color-surface);
  border-color: var(--color-border);
  color: var(--color-text-secondary);
  transition: all var(--transition-base);
}
.create-form :deep(.ant-radio-button-wrapper:not(.ant-radio-button-wrapper-disabled):hover) {
  color: var(--color-brand-hover);
  border-color: var(--color-border-strong);
}
.create-form :deep(.ant-radio-button-wrapper-checked:not(.ant-radio-button-wrapper-disabled)) {
  background: var(--color-brand-soft);
  border-color: var(--color-brand);
  color: var(--color-brand);
  box-shadow: -1px 0 0 var(--color-brand);
}
.create-form :deep(.ant-radio-button-wrapper-checked:not(.ant-radio-button-wrapper-disabled)::before) {
  background-color: var(--color-brand);
}
.radio-label-text {
  white-space: nowrap;
}

/* ==========================================
   Submit Area
   ========================================== */
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
  background: linear-gradient(135deg, #1677ff 0%, #4096ff 100%) !important;
  border: none !important;
  box-shadow: 0 4px 20px rgba(22, 119, 255, 0.3);
  transition: all var(--transition-base);
  letter-spacing: 0.02em;
}
.start-btn:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 6px 28px rgba(22, 119, 255, 0.45);
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

/* ==========================================
   Preview Panel
   ========================================== */
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
  border-color: var(--color-brand);
  box-shadow: var(--shadow-card), var(--shadow-glow);
}

/* 预览头部 */
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
  border-color: var(--color-brand);
}
.pulse-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-text-quaternary, rgba(0, 0, 0, 0.25));
  transition: all var(--transition-base);
}
.preview-pulse.active .pulse-dot {
  background: var(--color-brand);
  box-shadow: 0 0 10px rgba(64, 150, 255, 0.6);
  animation: pulse-dot 2s ease-in-out infinite;
}
@keyframes pulse-dot {
  0%, 100% { box-shadow: 0 0 6px rgba(64, 150, 255, 0.4); }
  50% { box-shadow: 0 0 16px rgba(64, 150, 255, 0.8); }
}

.preview-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text-secondary);
  letter-spacing: 0.02em;
}

/* 进度条 */
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
  background: linear-gradient(90deg, #1677ff, #4096ff);
  transition: width 0.5s cubic-bezier(0.4, 0, 0.2, 1);
}

/* 预览字段 */
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
}
.preview-value.placeholder {
  color: var(--color-text-muted);
}
.placeholder {
  color: var(--color-text-muted);
}

/* 预览标签 */
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

/* 预览空状态 */
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

/* ==========================================
   录音方式三列均分布局
   ========================================== */

.record-mode-row {
  display: grid;
  grid-template-columns: 1fr 1fr 1fr;
  gap: 12px;
  align-items: center;
  width: 100%;
}

/* radio-group 占前两列，内部按钮均分 */
.record-radio-group {
  grid-column: 1 / 3;
  display: flex;
  width: 100%;
}

.record-radio-group :deep(.ant-radio-button-wrapper) {
  flex: 1;
  text-align: center;
  justify-content: center;
}

/* 输入音量占第三列 */
.inline-volume {
  grid-column: 3 / 4;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.inline-volume-label {
  font-size: 12px;
  color: var(--color-text-muted);
  white-space: nowrap;
  flex-shrink: 0;
}

.sys-error-tip {
  flex: 0 0 100%;
  font-size: 11px;
  color: #ff4d4f;
  line-height: 1.3;
}

.volume-meter {
  flex: 1;
  height: 6px;
  min-width: 40px;
  border-radius: 3px;
  background: var(--color-fill-secondary, rgba(0, 0, 0, 0.06));
  overflow: hidden;
}

.volume-meter-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.08s linear;
  min-width: 2px;
}

/* ==========================================
   Responsive
   ========================================== */
@media (max-width: 900px) {
  .main-content {
    grid-template-columns: 1fr;
  }
  .preview-section {
    position: static;
    order: -1;
  }
  .preview-card {
    padding: 18px 20px;
  }
  .form-card :deep(.ant-card-body) {
    padding: 24px 20px 28px;
  }
  .inline-volume {
    grid-column: 3 / 4;
  }
}

@media (max-width: 480px) {
  .live-create {
    padding: 16px 12px 40px;
  }
  .title-row {
    gap: 12px;
  }
  .title-icon {
    width: 40px;
    height: 40px;
    border-radius: 10px;
    font-size: 20px;
  }
  .title-text h1 {
    font-size: 20px;
  }
  .title-text p {
    font-size: 12px;
  }
  .record-mode-row {
    grid-template-columns: 1fr 1fr 1fr;
    gap: 8px;
  }
}
</style>
