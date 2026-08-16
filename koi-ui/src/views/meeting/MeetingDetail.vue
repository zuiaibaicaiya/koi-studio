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
    </header>

    <!-- 会议转写（虚拟列表） -->
    <a-card class="transcript-card" variant="borderless" :styles="{ body: { padding: '0' } }">
      <template #title>
        <span class="transcript-title"><SoundOutlined /> 会议转写内容</span>
      </template>
      <div class="transcript-body">
        <a-empty
          v-if="!loading && transcripts.length === 0"
          description="暂无转写内容"
          class="transcript-empty"
        />
        <a-spin v-else-if="loading" description="加载转写内容…" class="transcript-loading" />

        <DynamicScroller
          v-else
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
              :size-dependencies="[item.text]"
            >
              <div class="transcript-item" :class="{ final: item.isFinal }" @click="seekTo(item)">
                <a-avatar class="speaker-avatar" :style="{ backgroundColor: item.color }">
                  {{ item.speaker.charAt(0) }}
                </a-avatar>
                <div class="transcript-content">
                  <div class="transcript-meta">
                    <span class="speaker-name">{{ item.speaker }}</span>
                    <span class="transcript-time">{{ item.clock }}</span>
                    <a-tag v-if="!item.isFinal" color="orange" class="draft-tag">识别中</a-tag>
                  </div>
                  <div class="transcript-text">{{ item.text }}</div>
                </div>
              </div>
            </DynamicScrollerItem>
          </template>
        </DynamicScroller>

        <div v-if="loadingMore" class="transcript-footer">
          <a-spin size="small" description="加载更多…" />
        </div>
        <div v-else-if="finished && transcripts.length" class="transcript-footer muted">
          已经到底啦，共 {{ total }} 条转写
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
          @click="skip(-5)"
          title="后退 5 秒"
        >
          <template #icon><BackwardOutlined /></template>
        </a-button>
        <a-button
          class="play-toggle"
          type="text"
          shape="circle"
          :disabled="!wave"
          @click="togglePlay"
          :aria-label="playing ? '暂停' : '播放'"
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
          @click="skip(5)"
          title="前进 5 秒"
        >
          <template #icon><ForwardOutlined /></template>
        </a-button>
      </div>
      <div class="audio-main">
        <div class="audio-meta">
          <span class="audio-time">{{ fmt(currentTime) }} / {{ fmt(duration) }}</span>
        </div>
        <div
          ref="waveRef"
          class="waveform"
          :class="{ loading: !ready }"
          @click="onWaveClick"
        ></div>
      </div>
      <a-select
        class="speed-select"
        :value="speed"
        :disabled="!wave"
        size="small"
        :popup-match-select-width="false"
        :options="speeds.map((s) => ({ value: s, label: `${s}x` }))"
        @change="onSpeedChange"
      />
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue';
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
} from '@antdv-next/icons';
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller';
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css';
import WaveSurfer from 'wavesurfer.js';
import { meetingApi, type MeetingDTO, type MeetingTranscriptDTO } from '../../services/meetingApi';

const { message } = App.useApp();

const route = useRoute();
const router = useRouter();

const meetingId = computed(() => Number(route.params.id));

const meeting = ref<MeetingDTO | null>(null);
const meetingLoading = ref(false);
const transcripts = ref<TranscriptItem[]>([]);
const loading = ref(false);
const loadingMore = ref(false);
const page = ref(1);
const pageSize = ref(50);
const total = ref(0);

const totalPages = computed(() => (total.value ? Math.max(1, Math.ceil(total.value / pageSize.value)) : 1));
const finished = computed(() => page.value >= totalPages.value);

interface TranscriptItem {
  id: number;
  speaker: string;
  text: string;
  startMs: number;
  endMs: number;
  isFinal: boolean;
  clock: string;
  color: string;
}

const STATUS: Record<MeetingDTO['status'], { text: string; color: string }> = {
  created: { text: '待开始', color: 'default' },
  ongoing: { text: '进行中', color: 'processing' },
  finished: { text: '已结束', color: 'success' },
};
const statusText = (s: MeetingDTO['status']) => STATUS[s]?.text ?? '未知';
const statusColor = (s: MeetingDTO['status']) => STATUS[s]?.color ?? 'default';

const SPEAKER_COLORS = ['#2dd4bf', '#06b6d4', '#8b5cf6', '#f59e0b', '#ef4444', '#10b981', '#ec4899', '#3b82f6'];
function speakerColor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return SPEAKER_COLORS[h % SPEAKER_COLORS.length];
}

function utteranceClock(startMs: number): string {
  const base = meeting.value?.start_time;
  if (base) {
    const d = new Date(String(base).replace(' ', 'T'));
    if (!Number.isNaN(d.getTime())) {
      const t = new Date(d.getTime() + startMs);
      const pad = (n: number) => String(n).padStart(2, '0');
      return `${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}`;
    }
  }
  return '00:00';
}

function toSegment(t: MeetingTranscriptDTO): TranscriptItem {
  const speaker = t.speaker_name || '未知说话人';
  return {
    id: t.id,
    speaker,
    text: t.text || '(空)',
    startMs: t.start_ms,
    endMs: t.end_ms,
    isFinal: t.is_final,
    clock: utteranceClock(t.start_ms),
    color: speakerColor(speaker),
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
  page.value = 1;
  try {
    const res = await meetingApi.getMeetingTranscripts(meetingId.value, { page: 1, pageSize: pageSize.value });
    transcripts.value = res.items.map(toSegment);
    total.value = res.total;
    page.value = res.page;
  } catch (e) {
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
const waveRef = ref<HTMLElement | null>(null);
const wave = shallowRef<WaveSurfer | null>(null);
const audioSrc = computed(() => meeting.value?.audio_url || '');
const playing = ref(false);
const ready = ref(false);
const currentTime = ref(0);
const duration = ref(0);
const speeds = [0.75, 1, 1.25, 1.5, 2];
const speedIndex = ref(0);
const speed = computed(() => speeds[speedIndex.value]);

function fmt(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) sec = 0;
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

function destroyWave() {
  if (wave.value) {
    wave.value.destroy();
    wave.value = null;
  }
  playing.value = false;
  ready.value = false;
  currentTime.value = 0;
  duration.value = 0;
}

function initWave() {
  destroyWave();
  const el = waveRef.value;
  if (!el || !audioSrc.value) return;
  const ws = WaveSurfer.create({
    container: el,
    height: 56,
    waveColor: 'rgba(99, 102, 241, 0.35)',
    progressColor: '#6366f1',
    cursorColor: '#f59e0b',
    cursorWidth: 2,
    barWidth: 2,
    barGap: 2,
    barRadius: 2,
    url: audioSrc.value,
  });
  ws.on('ready', () => {
    ready.value = true;
    duration.value = ws.getDuration();
    ws.setPlaybackRate(speed.value, false);
  });
  ws.on('timeupdate', (t: number) => {
    currentTime.value = t;
  });
  ws.on('play', () => (playing.value = true));
  ws.on('pause', () => (playing.value = false));
  ws.on('finish', () => {
    playing.value = false;
    currentTime.value = 0;
  });
  ws.on('error', () => message.warning('音频加载失败，无法播放'));
  wave.value = ws;
}

function togglePlay() {
  if (!wave.value) return;
  wave.value.playPause();
}
function seekTo(item: TranscriptItem) {
  if (!audioSrc.value) {
    message.info('该会议暂无音频');
    return;
  }
  const ws = wave.value;
  if (!ws) return;
  ws.setTime(item.startMs / 1000);
  if (!ws.isPlaying()) ws.play().catch(() => message.warning('音频加载失败，无法播放'));
}
function onWaveClick() {
  // 点击波形切换播放/暂停
  togglePlay();
}
function skip(seconds: number) {
  const ws = wave.value;
  if (!ws || !duration.value) return;
  ws.setTime(Math.min(Math.max(ws.getCurrentTime() + seconds, 0), duration.value));
}
function onSpeedChange(v: number) {
  const i = speeds.indexOf(v);
  if (i >= 0) {
    speedIndex.value = i;
    wave.value?.setPlaybackRate(speed.value, false);
  }
}

function goBack() {
  router.push('/system/meetings');
}

onMounted(async () => {
  await loadMeeting();
  await loadTranscripts();
});

onUnmounted(() => {
  destroyWave();
});

// 当前会议切换时重建波形（flush:'post' 确保在 DOM 渲染、waveRef 绑定后再初始化）
watch(
  () => meeting.value?.audio_url,
  (url) => {
    if (url) initWave();
    else destroyWave();
  },
  { flush: 'post' },
);
</script>

<style scoped>
.meeting-detail {
  max-width: 1080px;
  margin: 0 auto;
  padding: 16px 20px 96px;
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
  margin-bottom: 16px;
  border-radius: 12px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
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
  min-height: 320px;
}
.transcript-empty,
.transcript-loading {
  padding: 80px 0;
  display: flex;
  justify-content: center;
}
.transcript-scroller {
  height: calc(100vh - 460px);
  min-height: 320px;
  padding: 8px 16px;
}
.transcript-footer {
  text-align: center;
  padding: 12px 0 16px;
  color: var(--color-text-muted);
}
.transcript-footer.muted {
  font-size: 12px;
}

.transcript-item {
  display: flex;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--color-border);
  cursor: pointer;
  transition: background 0.15s ease;
}
.transcript-item:hover {
  background: var(--color-hover, rgba(99, 102, 241, 0.06));
}

.audio-bar {
  position: fixed;
  left: 50%;
  transform: translateX(-50%);
  bottom: 16px;
  width: calc(100% - 40px);
  max-width: 1080px;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 20px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: 12px;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.16);
  backdrop-filter: blur(8px);
  z-index: 50;
}
.audio-bar.playing {
  border-color: var(--color-primary, #6366f1);
}
.audio-controls {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 0 0 auto;
}
.audio-bar .play-toggle,
.audio-bar .ctrl-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--color-primary, #6366f1);
  transition: transform 0.12s ease, color 0.12s ease;
}
.audio-bar .ctrl-btn {
  font-size: 18px;
  width: 34px;
  height: 34px;
}
.audio-bar .play-toggle {
  font-size: 34px;
  width: 46px;
  height: 46px;
}
.audio-bar :not(:disabled).ctrl-btn:hover,
.audio-bar :not(:disabled).play-toggle:hover {
  transform: scale(1.12);
  color: var(--color-primary-hover, #4f46e5);
}
.audio-bar .ctrl-btn :deep(svg) {
  width: 1em;
  height: 1em;
}
.audio-bar .play-toggle :deep(svg) {
  width: 1em;
  height: 1em;
}
.audio-main {
  flex: 1;
  min-width: 0;
}
.audio-meta {
  display: flex;
  align-items: baseline;
  margin-bottom: 4px;
}
.audio-time {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--color-text-muted);
  font-variant-numeric: tabular-nums;
}
.waveform {
  width: 100%;
  min-height: 56px;
  cursor: pointer;
}
.waveform.loading {
  background: linear-gradient(
    90deg,
    rgba(99, 102, 241, 0.08) 25%,
    rgba(99, 102, 241, 0.16) 37%,
    rgba(99, 102, 241, 0.08) 63%
  );
  background-size: 400% 100%;
  border-radius: 6px;
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
.audio-bar .speed-select {
  flex: 0 0 auto;
  width: 78px;
}
.audio-bar .speed-select :deep(.ant-select-selector) {
  border-radius: 6px;
  font-variant-numeric: tabular-nums;
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
  line-height: 1.6;
  word-break: break-word;
  white-space: pre-wrap;
}
</style>
