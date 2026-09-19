<template>
  <div class="meeting-detail">
    <!-- 顶部栏 -->
    <header class="detail-topbar">
      <a-button class="back-btn" type="text" @click="goBack">
        <template #icon><ArrowLeftOutlined /></template>
        返回
      </a-button>
      <div class="topbar-title">
        <a-skeleton v-if="meetingLoading" :paragraph="false" :title="{ width: 240 }" active />
        <template v-else>
          <h1>{{ meeting?.name || '会议详情' }}</h1>
          <div class="topbar-meta">
            <a-tag :color="meeting ? statusColor(meeting.status) : 'default'">
              {{ meeting ? statusText(meeting.status) : '' }}
            </a-tag>
            <span class="meta-item"><ClockCircleOutlined /> {{ timeRange }}</span>
            <span class="meta-item"><TeamOutlined /> {{ participantList.length }} 人</span>
            <span class="meta-item"><FileTextOutlined /> {{ total }} 条转写</span>
          </div>
        </template>
      </div>
      <a-button
        class="retrans-btn"
        :loading="retranscribing"
        :disabled="meetingLoading || !meeting || !meeting.audio_url"
        title="基于当前会议的音频文件重新转写"
        @click="handleRetranscribe"
      >
        <template #icon><ReloadOutlined /></template>
        重新转写
      </a-button>
      <a-button
        class="export-btn"
        type="primary"
        :loading="exporting"
        :disabled="meetingLoading || !meeting"
        @click="handleExport"
      >
        <template #icon><DownloadOutlined /></template>
        导出
      </a-button>
    </header>

    <!-- 重新转写进度条 -->
    <div v-if="rt.visible" class="rt-strip" :class="rt.status">
      <div class="rt-head">
        <ReloadOutlined v-if="rt.status === 'pending' || rt.status === 'running'" spin />
        <CheckCircleFilled v-else-if="rt.status === 'completed'" />
        <CloseCircleFilled v-else />
        <span class="rt-label">
          {{
            rt.status === 'completed'
              ? '重新转写完成'
              : rt.status === 'failed'
                ? '重新转写失败'
                : '重新转写中…'
          }}
        </span>
        <span class="rt-pct">{{ rt.progress }}%</span>
      </div>
      <a-progress
        class="rt-bar"
        :percent="rt.progress"
        :status="rt.status === 'failed' ? 'exception' : rt.status === 'completed' ? 'success' : 'active'"
        :show-info="false"
        size="small"
      />
      <div class="rt-step">
        {{ rt.step || '' }}<span v-if="rt.error" class="rt-error"> · {{ rt.error }}</span>
      </div>
    </div>

    <!-- 会议转写（虚拟列表） -->
    <a-card class="transcript-card" variant="borderless" :styles="{ body: { padding: '0' } }">
      <template #title>
        <span class="transcript-title"><SoundOutlined /> 会议转写内容</span>
      </template>
      <div class="transcript-body">
        <!-- 加载失败：给出原因与重试入口，而不是误显示为空内容 -->
        <div v-if="loadError" class="transcript-error">
          <p>转写内容加载失败，请检查网络后重试。</p>
          <a-button size="small" @click="loadTranscripts">重试</a-button>
        </div>
        <a-empty
          v-else-if="!loading && transcripts.length === 0"
          description="暂无转写内容"
          class="transcript-empty"
        />
        <a-spin v-else-if="loading" description="加载转写内容…" class="transcript-loading" />

        <DynamicScroller
          v-else
          ref="scrollerRef"
          class="transcript-scroller"
          :items="transcripts"
          :min-item-size="64"
          key-field="id"
          @scroll="onScroll"
        >
          <template #default="{ item, index }">
            <DynamicScrollerItem
              :item="item"
              :active="true"
              :data-index="index"
            >
              <div
                class="transcript-item"
                :class="{ final: item.isFinal, 'no-audio': !audioSrc }"
                :title="audioSrc ? '点击播放此句' : ''"
                @click="seekTo(item)"
              >
                <a-avatar
                  class="speaker-avatar"
                  :style="{ backgroundColor: speakerPalette[item.colorIndex] }"
                >
                  {{ item.speaker.charAt(0) }}
                </a-avatar>
                <div class="transcript-content">
                  <div class="transcript-meta">
                    <span class="speaker-name">{{ item.speaker }}</span>
                    <span class="transcript-time">{{ item.clock }}</span>
                    <a-tag v-if="!item.isFinal" color="orange" class="draft-tag">识别中</a-tag>
                  </div>
                  <div class="transcript-text">
                    <template v-if="item.words && item.words.length && item.words[0]">
                      <template v-for="(span, wi) in item.words" :key="wi">
                        <span
                          v-show="span"
                          class="word-span"
                          :class="wordState(item, span)"
                          :title="`${formatPreciseMs(span.startMs)} → ${formatPreciseMs(span.endMs)}`"
                          @click.stop="seekToSpan(item, span)"
                          >{{ span.word }}</span
                        ><span v-if="span && !isCJK(span.word)" class="word-space"> </span>
                      </template>
                    </template>
                    <template v-else>{{ item.text }}</template>
                  </div>
                </div>
              </div>
            </DynamicScrollerItem>
          </template>
        </DynamicScroller>

        <div v-if="loadingMore" class="transcript-footer">
          <a-spin size="small" description="加载更多…" />
        </div>
      </div>
    </a-card>

    <!-- 底部音频播放器（wavesurfer.js） -->
    <footer v-if="audioSrc" class="audio-bar" :class="{ playing }">
      <div class="audio-controls">
        <a-button
          class="ctrl-btn"
          type="text"
          shape="circle"
          :disabled="!wave"
          title="后退 5 秒"
          aria-label="后退 5 秒"
          @click="skip(-5)"
        >
          <template #icon><BackwardOutlined /></template>
        </a-button>
        <a-button
          class="play-toggle"
          type="text"
          shape="circle"
          :disabled="!wave"
          :title="playing ? '暂停' : '播放'"
          :aria-label="playing ? '暂停' : '播放'"
          @click="togglePlay"
        >
          <template #icon>
            <PauseCircleOutlined v-if="playing" />
            <PlayCircleOutlined v-else />
          </template>
        </a-button>
        <a-button
          class="ctrl-btn"
          type="text"
          shape="circle"
          :disabled="!wave"
          title="前进 5 秒"
          aria-label="前进 5 秒"
          @click="skip(5)"
        >
          <template #icon><ForwardOutlined /></template>
        </a-button>
      </div>
      <div class="audio-main">
        <div class="audio-meta">
          <span class="audio-time current">{{ formatClock(currentTime) }}</span>
          <span class="audio-hint">空格 播放/暂停 · ←/→ 5 秒</span>
          <span class="audio-time total">{{ formatClock(duration) }}</span>
        </div>
        <div
          ref="waveRef"
          class="waveform"
          :class="{ loading: !ready }"
          :title="ready ? '点击波形定位播放位置' : '音频加载中…'"
        ></div>
      </div>
      <div class="audio-side">
        <!-- 倍速下拉：弹层给足宽度（避免选项文字被省略），右对齐使其向左展开，不压到音量键 -->
        <a-select
          class="speed-select"
          :value="speed"
          :disabled="!wave"
          size="small"
          variant="borderless"
          placement="topRight"
          :popup-match-select-width="108"
          :options="speeds.map((s) => ({ value: s, label: `${s}x` }))"
          @change="onSpeedChange"
        />
        <a-button
          class="volume-btn"
          type="text"
          shape="circle"
          :disabled="!wave"
          :title="muted ? '取消静音' : '静音'"
          :aria-label="muted ? '取消静音' : '静音'"
          @click="toggleMute"
        >
          <template #icon>
            <MutedOutlined v-if="muted || volume === 0" />
            <SoundOutlined v-else />
          </template>
        </a-button>
        <a-slider
          class="volume-slider"
          :value="muted ? 0 : volume"
          :min="0"
          :max="1"
          :step="0.05"
          :disabled="!wave"
          :tooltip="{ open: false }"
          @change="onVolumeChange"
        />
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { App } from 'antdv-next';
import {
  ArrowLeftOutlined,
  ClockCircleOutlined,
  TeamOutlined,
  FileTextOutlined,
  SoundOutlined,
  PlayCircleOutlined,
  PauseCircleOutlined,
  BackwardOutlined,
  ForwardOutlined,
  DownloadOutlined,
  ReloadOutlined,
  CheckCircleFilled,
  CloseCircleFilled,
  MutedOutlined,
} from '@antdv-next/icons';
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller';
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css';
import { meetingApi, type MeetingDTO, type MeetingTranscriptDTO } from '../../services/meetingApi';
import { exportMeetingById } from '../../utils/exportMeeting';
import { formatClock, formatOffset, formatPreciseMs } from '../../utils/time';
import { isCJK, parseWordTimestamps } from '../../utils/wordTimestamps';
import type { TranscriptItem, WordSpan } from '../../types/transcript';
import { speakerColorIndex, useSpeakerPalette } from '../../composables/useSpeakerPalette';
import { useAudioPlayer } from '../../composables/useAudioPlayer';
import { useRetranscribe } from '../../composables/useRetranscribe';

const { message } = App.useApp();

const route = useRoute();
const router = useRouter();

const meetingId = computed(() => Number(route.params.id));

const meeting = ref<MeetingDTO | null>(null);
const meetingLoading = ref(false);
const transcripts = ref<TranscriptItem[]>([]);
const loading = ref(false);
/** 转写内容加载失败标记：失败时展示错误态与重试，而不是误显示为空内容 */
const loadError = ref(false);
const loadingMore = ref(false);
const page = ref(1);
const pageSize = ref(50);
const total = ref(0);

const totalPages = computed(() => (total.value ? Math.max(1, Math.ceil(total.value / pageSize.value)) : 1));
const finished = computed(() => page.value >= totalPages.value);

const STATUS: Record<MeetingDTO['status'], { text: string; color: string }> = {
  created: { text: '待开始', color: 'default' },
  ongoing: { text: '进行中', color: 'processing' },
  finished: { text: '已结束', color: 'success' },
};
const statusText = (s: MeetingDTO['status']) => STATUS[s]?.text ?? '未知';
const statusColor = (s: MeetingDTO['status']) => STATUS[s]?.color ?? 'default';

// ---- 说话人配色：色相与饱和度取自当前主题色，明度深浅分档，色系/明暗切换自动重算 ----
const speakerPalette = useSpeakerPalette();

function toSegment(t: MeetingTranscriptDTO): TranscriptItem {
  const speaker = t.speaker_name || '未知说话人';
  const words = parseWordTimestamps(t);
  return {
    id: t.id,
    speaker,
    text: t.text || '(空)',
    startMs: t.start_ms,
    endMs: t.end_ms,
    isFinal: t.is_final,
    clock: t.end_ms > t.start_ms ? `${formatOffset(t.start_ms)} - ${formatOffset(t.end_ms)}` : formatOffset(t.start_ms),
    colorIndex: speakerColorIndex(speaker),
    words,
  };
}

const timeRange = computed(() => {
  if (!meeting.value) return '—';
  const s = meeting.value.start_time;
  const e = meeting.value.end_time;
  return e ? `${s} ~ ${e}` : s;
});

const participantList = computed(() =>
  (meeting.value?.participants || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean),
);

async function loadMeeting() {
  meetingLoading.value = true;
  try {
    meeting.value = await meetingApi.getMeeting(meetingId.value);
  } catch (e) {
    message.error((e as { message?: string })?.message || '加载会议信息失败');
  } finally {
    meetingLoading.value = false;
  }
}

async function loadTranscripts() {
  loading.value = true;
  loadError.value = false;
  page.value = 1;
  try {
    const res = await meetingApi.getMeetingTranscripts(meetingId.value, { page: 1, pageSize: pageSize.value });
    transcripts.value = res.items.map(toSegment);
    total.value = res.total;
    page.value = res.page;
  } catch (e) {
    loadError.value = true;
    message.error((e as { message?: string })?.message || '加载转写内容失败');
  } finally {
    loading.value = false;
  }
}

async function loadMore() {
  if (loading.value || loadingMore.value || finished.value) return;
  loadingMore.value = true;
  try {
    const res = await meetingApi.getMeetingTranscripts(meetingId.value, { page: page.value + 1, pageSize: pageSize.value });
    transcripts.value.push(...res.items.map(toSegment));
    total.value = res.total;
    page.value = res.page;
  } catch (e) {
    message.error((e as { message?: string })?.message || '加载更多转写内容失败');
  } finally {
    loadingMore.value = false;
  }
}

function onScroll(event: Event) {
  const el = event.target as HTMLElement;
  if (!el.classList?.contains('vue-recycle-scroller')) return;
  if (el.scrollHeight - el.scrollTop - el.clientHeight < 200) loadMore();
}

// 音频播放（wavesurfer.js）
/** 音频 URL：追加基于会议更新时间的版本号（如 /a.wav?v=1724...），
 *  重新上传/更新音频后 updated_at 变化，浏览器会重新拉取音频而非命中旧缓存。 */
const audioSrc = computed(() => {
  const url = meeting.value?.audio_url;
  if (!url) return '';
  const version = audioVersion.value;
  if (!version) return url;
  return `${url}${url.includes('?') ? '&' : '?'}v=${version}`;
});

/** 音频版本号：优先取会议更新时间（毫秒时间戳），缺失时用当前时间兜底 */
const audioVersion = computed(() => {
  const updatedAt = meeting.value?.updatedAt;
  if (updatedAt) {
    const ts = Date.parse(updatedAt);
    if (Number.isFinite(ts)) return String(ts);
  }
  return String(Date.now());
});

const {
  waveRef,
  wave,
  playing,
  ready,
  currentTime,
  duration,
  currentMs,
  playAnchorMs,
  speed,
  speeds,
  volume,
  muted,
  togglePlay,
  seekToMs,
  skip,
  toggleMute,
  onVolumeChange,
  onSpeedChange,
  destroy: destroyPlayer,
} = useAudioPlayer(audioSrc, { onError: (msg) => message.warning(msg) });

function seekTo(item: TranscriptItem) {
  if (!audioSrc.value) {
    message.info('该会议暂无音频');
    return;
  }
  seekToMs(item.startMs);
}

/** 点击某词/字，从该词对应的精确时间点开始播放；高亮从此字开始 */
function seekToSpan(item: TranscriptItem, span: WordSpan) {
  if (!audioSrc.value) {
    message.info('该会议暂无音频');
    return;
  }
  seekToMs(span.startMs);
}

// ---- 播放中的段落定位：高亮当前句并自动滚动，方便长会议跟读 ----
const scrollerRef = ref<InstanceType<typeof DynamicScroller> | null>(null);
/** 最近一次自动滚动到的段落 id，避免同一句反复触发 scrollToItem */
let autoScrolledId = 0;

const activeSegmentId = computed(() => {
  const ms = currentMs.value;
  if (ms <= 0) return 0;
  // 取播放头所在的段落（startMs <= ms < endMs）；落在句间静音时沿用上一句
  let active = 0;
  for (const item of transcripts.value) {
    if (item.startMs <= ms) {
      if (ms < item.endMs) return item.id;
      active = item.id;
    } else {
      break;
    }
  }
  return active;
});

watch(activeSegmentId, (id) => {
  if (!id || !playing.value || id === autoScrolledId) return;
  autoScrolledId = id;
  const idx = transcripts.value.findIndex((t) => t.id === id);
  if (idx >= 0) scrollerRef.value?.scrollToItem(idx);
});
// 暂停/结束后不再自动滚动，但保留当前句高亮；新的一轮播放重置去重标记
watch(playing, (p) => {
  if (p) autoScrolledId = 0;
});

/** 判断某个词/字的高亮状态：active=播放头当前所在字，played=已播放过（从本次播放起点开始），''=未播放 */
function wordState(item: TranscriptItem, span: WordSpan): string {
  // 已播放：从该次播放起点(anchor)起、且已被播放头越过结尾的字
  if (span.startMs >= playAnchorMs.value && span.endMs <= currentMs.value) return 'played';
  // 播放头当前所在字（无论是否暂停都指示当前进度位置）
  if (currentMs.value >= span.startMs && currentMs.value < span.endMs) return 'active';
  return '';
}

/** 含中文字符的词，渲染时无需额外空格 */
function isCJK(w: string): boolean {
  return /[一-鿿]/.test(w);
}

/**
 * 键盘快捷键：空格 播放/暂停，←/→ 前后 5 秒。
 * 输入框、下拉选择与按钮内的按键交还给组件自身处理（按钮用空格激活自身，不能被拦截两次）。
 */
function onKeydown(e: KeyboardEvent) {
  if (!wave.value || e.metaKey || e.ctrlKey || e.altKey) return;
  const target = e.target as HTMLElement | null;
  if (target?.closest('input, textarea, [contenteditable], .ant-select, .ant-select-dropdown')) return;
  if (e.key === ' ' || e.code === 'Space') {
    if (target?.closest('button, a[href]')) return;
    e.preventDefault();
    togglePlay();
  } else if (e.key === 'ArrowLeft') {
    e.preventDefault();
    skip(-5);
  } else if (e.key === 'ArrowRight') {
    e.preventDefault();
    skip(5);
  }
}

function goBack() {
  // 优先回退到来源页（如从搜索/其他入口进入），无历史时再回会议列表
  if (window.history.state?.back) {
    router.back();
  } else {
    router.push('/system/meetings');
  }
}

const exporting = ref(false);
async function handleExport() {
  if (!meeting.value) return;
  exporting.value = true;
  try {
    const { audioIncluded } = await exportMeetingById(meeting.value.id);
    message.success(audioIncluded ? '导出成功' : '已导出会议文本（音频下载失败）');
  } catch (e) {
    console.error(e);
    message.error('导出失败：' + (e instanceof Error ? e.message : String(e)));
  } finally {
    exporting.value = false;
  }
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown);
  await loadMeeting();
  await loadTranscripts();
});

// ---- 重新转写（实时 / 离线会议均基于已归档音频调用离线转写） ----
const { state: rt, retranscribing, start: startRetranscribe } = useRetranscribe({
  meetingId: () => meetingId.value,
  // 停止音频播放并重置页面状态（避免旧转录/播放状态残留）
  onBegin: () => {
    destroyPlayer();
    transcripts.value = [];
  },
  onCompleted: async () => {
    await Promise.all([loadTranscripts(), loadMeeting()]);
    message.success('重新转写完成');
  },
  onError: (msg) => message.error(msg),
});

async function handleRetranscribe() {
  if (!meeting.value || retranscribing.value || !meeting.value.audio_url) return;
  await startRetranscribe();
}

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown);
  destroyPlayer();
});
</script>

<style scoped>
.meeting-detail {
  max-width: 1080px;
  margin: 0 auto;
  padding: 16px 20px 16px;
  height: calc(100vh - var(--titlebar-height));
  display: flex;
  flex-direction: column;
  box-sizing: border-box;
}

.detail-topbar {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 16px;
}
.detail-topbar .back-btn {
  margin-top: 4px;
}
.detail-topbar .export-btn {
  margin-top: 4px;
  flex-shrink: 0;
}
.detail-topbar .retrans-btn {
  margin-top: 4px;
  flex-shrink: 0;
}

/* 重新转写进度条 */
.rt-strip {
  margin-bottom: 16px;
  padding: 12px 16px;
  border-radius: 12px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
}
.rt-strip.completed {
  border-color: rgba(16, 185, 129, 0.5);
}
.rt-strip.failed {
  border-color: rgba(239, 68, 68, 0.5);
}
.rt-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.rt-head .anticon {
  color: var(--color-primary, #6366f1);
}
.rt-strip.completed .rt-head .anticon {
  color: #10b981;
}
.rt-strip.failed .rt-head .anticon {
  color: #ef4444;
}
.rt-label {
  font-weight: 600;
  color: var(--color-text);
}
.rt-pct {
  margin-left: auto;
  font-variant-numeric: tabular-nums;
  color: var(--color-text-muted);
  font-size: 13px;
}
.rt-bar {
  margin-bottom: 4px;
}
.rt-step {
  font-size: 12px;
  color: var(--color-text-muted);
  word-break: break-word;
}
.rt-error {
  color: #ef4444;
}
.topbar-title {
  flex: 1;
}
.topbar-title h1 {
  font-size: 20px;
  font-weight: 700;
  margin: 0 0 6px;
  color: var(--color-text);
}
.topbar-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 14px;
  color: var(--color-text-muted);
  font-size: 13px;
}
.topbar-meta .meta-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.transcript-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  margin-bottom: 12px;
  border-radius: 12px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
}
/* 卡片内容区撑满剩余高度，让转写列表与底部播放器紧挨 */
.transcript-card :deep(.ant-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.transcript-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--color-text);
  font-weight: 600;
}

.transcript-body {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.transcript-empty,
.transcript-loading {
  padding: 80px 0;
  display: flex;
  justify-content: center;
}
.transcript-scroller {
  flex: 1;
  min-height: 320px;
  padding: 8px 16px;
}
.transcript-footer {
  text-align: center;
  padding: 12px 0 16px;
  color: var(--color-text-muted);
}

.transcript-item {
  display: flex;
  gap: 12px;
  padding: 10px 6px;
  border-bottom: 1px solid var(--color-border);
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s ease;
}
.transcript-item:hover {
  /* 用品牌色实时混色，替代原先恒为靛蓝的 --color-hover 兜底值 */
  background: color-mix(in srgb, var(--color-brand) 6%, transparent);
}
/* 无音频：点击只会得到提示，展示为普通文本光标，不再误导可点击播放 */
.transcript-item.no-audio {
  cursor: text;
}

/* 转写加载失败态 */
.transcript-error {
  padding: 64px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  color: var(--color-text-muted);
}

.audio-bar {
  /* 不再悬浮：随文档流紧跟在转写卡片下方 */
  position: static;
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 10px 16px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-xl, 12px);
  /* 阴影走令牌：深色下不再是一层发灰的黑影 */
  box-shadow: var(--shadow-md);
  transition: border-color 0.2s ease;
  z-index: 50;
}
.audio-bar.playing {
  border-color: color-mix(in srgb, var(--color-brand) 45%, var(--color-border));
}
.audio-controls {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
}
.audio-bar .play-toggle,
.audio-bar .ctrl-btn,
.audio-bar .volume-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--color-brand);
  transition: transform 0.12s ease, color 0.12s ease, background 0.12s ease;
}
.audio-bar .ctrl-btn {
  font-size: 18px;
  /* 前后 5 秒：横向加宽的胶囊形，圆形会被拉成椭圆 */
  width: 46px;
  height: 34px;
  border-radius: 999px;
}
.audio-bar .play-toggle {
  font-size: 36px;
  width: 52px;
  height: 52px;
}
.audio-bar .volume-btn {
  font-size: 17px;
  width: 34px;
  height: 34px;
}
/* 播放中的状态：播放键带品牌色浅底，明暗两种外观下都成立 */
.audio-bar.playing .play-toggle {
  background: color-mix(in srgb, var(--color-brand) 16%, transparent);
}
.audio-bar :not(:disabled).ctrl-btn:hover,
.audio-bar :not(:disabled).play-toggle:hover,
.audio-bar :not(:disabled).volume-btn:hover {
  transform: scale(1.12);
  color: var(--color-brand-hover);
}
.audio-bar :not(:disabled).ctrl-btn:active,
.audio-bar :not(:disabled).play-toggle:active,
.audio-bar :not(:disabled).volume-btn:active {
  transform: scale(0.96);
}
/* 键盘可达：所有播放控制键有明确的焦点环 */
.audio-bar .ctrl-btn:focus-visible,
.audio-bar .play-toggle:focus-visible,
.audio-bar .volume-btn:focus-visible {
  outline: 2px solid var(--color-brand);
  outline-offset: 2px;
}
.audio-bar .ctrl-btn :deep(svg),
.audio-bar .play-toggle :deep(svg),
.audio-bar .volume-btn :deep(svg) {
  width: 1em;
  height: 1em;
}
.audio-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
/* 时间轴：已播放时间靠左、总时长靠右，中间留给快捷键提示 */
.audio-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  font-variant-numeric: tabular-nums;
}
.audio-time {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--color-text-muted);
}
.audio-bar.playing .audio-time.current {
  color: var(--color-brand);
  font-weight: 600;
}
.audio-hint {
  flex: 1;
  min-width: 0;
  text-align: center;
  font-size: 12px;
  color: var(--color-text-quaternary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.waveform {
  width: 100%;
  min-height: 44px;
  cursor: pointer;
}
.waveform.loading {
  background: linear-gradient(
    90deg,
    color-mix(in srgb, var(--color-brand) 8%, transparent) 25%,
    color-mix(in srgb, var(--color-brand) 18%, transparent) 37%,
    color-mix(in srgb, var(--color-brand) 8%, transparent) 63%
  );
  background-size: 400% 100%;
  border-radius: var(--radius-md, 6px);
  animation: wave-skeleton 1.4s ease infinite;
}
@keyframes wave-skeleton {
  0% {
    background-position: 100% 50%;
  }
  100% {
    background-position: 0 50%;
  }
}
.audio-side {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 0 0 auto;
}
.audio-bar .speed-select {
  flex: 0 0 auto;
  width: 92px;
}
.audio-bar .speed-select :deep(.ant-select-selector) {
  border-radius: var(--radius-md, 6px);
  font-variant-numeric: tabular-nums;
  padding-inline: 10px;
}
.volume-slider {
  width: 84px;
  margin: 0 2px;
}
/* 窄窗口优先保证波形与时间的完整：提示文案与音量滑杆先让位 */
@media (max-width: 860px) {
  .audio-hint,
  .volume-slider {
    display: none;
  }
}
.speaker-avatar {
  flex: 0 0 auto;
  color: #fff;
  font-weight: 600;
}
.transcript-content {
  flex: 1;
  min-width: 0;
}
.transcript-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 2px;
}
.speaker-name {
  font-weight: 600;
  color: var(--color-text);
}
.transcript-time {
  font-size: 12px;
  color: var(--color-text-muted);
  font-variant-numeric: tabular-nums;
}
.draft-tag {
  transform: scale(0.9);
}
.transcript-text {
  color: var(--color-text);
  line-height: 1.8;
  word-break: break-word;
  white-space: pre-wrap;
}
/* 逐字/逐词高亮：可点击定位，播放中高亮当前字，已播放字常驻高亮 */
.word-span {
  cursor: pointer;
  border-radius: 3px;
  padding: 0 1px;
  transition: background 0.12s ease, color 0.12s ease;
}
.word-span:hover {
  background: color-mix(in srgb, var(--color-brand) 12%, transparent);
}
.word-span.played {
  color: var(--color-brand);
  background: color-mix(in srgb, var(--color-brand) 10%, transparent);
}
.word-span.active {
  color: var(--color-text-inverse);
  background: var(--color-brand);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--color-brand) 28%, transparent);
  font-weight: 600;
}
/* 英文词之间的分隔空格，避免连续英文粘连 */
.word-space {
  white-space: pre-wrap;
}
</style>
