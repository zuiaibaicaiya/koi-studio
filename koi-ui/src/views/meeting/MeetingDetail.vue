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
                          :title="`${fmtMs(span.startMs)} → ${fmtMs(span.endMs)}`"
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
          <span class="audio-time current">{{ fmt(currentTime) }}</span>
          <span class="audio-hint">空格 播放/暂停 · ←/→ 5 秒</span>
          <span class="audio-time total">{{ fmt(duration) }}</span>
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
  DownloadOutlined,
  ReloadOutlined,
  CheckCircleFilled,
  CloseCircleFilled,
  MutedOutlined,
} from '@antdv-next/icons';
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller';
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css';
import WaveSurfer from 'wavesurfer.js';
import { meetingApi, type MeetingDTO, type MeetingTranscriptDTO } from '../../services/meetingApi';
import { exportMeetingById } from '../../utils/exportMeeting';
import { useThemeStore } from '../../store/theme';
import { presetMeta } from '../../theme/presets';

const { message } = App.useApp();

const route = useRoute();
const router = useRouter();
const themeStore = useThemeStore();

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

/** 转写中的最小时间单元（中文按字、英文按词），带相对于音频开头的起止毫秒 */
interface WordSpan {
  word: string;
  startMs: number;
  endMs: number;
}

interface TranscriptItem {
  id: number;
  speaker: string;
  text: string;
  startMs: number;
  endMs: number;
  isFinal: boolean;
  clock: string;
  /** 说话人配色下标（见 speakerPalette）：颜色随主题实时变化，故只存下标 */
  colorIndex: number;
  /** 词级时间轴；为空表示后端未提供 word_timestamps，此时整体使用段级时间 */
  words: WordSpan[];
}

const STATUS: Record<MeetingDTO['status'], { text: string; color: string }> = {
  created: { text: '待开始', color: 'default' },
  ongoing: { text: '进行中', color: 'processing' },
  finished: { text: '已结束', color: 'success' },
};
const statusText = (s: MeetingDTO['status']) => STATUS[s]?.text ?? '未知';
const statusColor = (s: MeetingDTO['status']) => STATUS[s]?.color ?? 'default';

// ---- 说话人配色：直接用主题色（当前色系的品牌色），而不是固定的一串颜色 ----
/** 说话人头像分档数：同一主题色下用明度深浅区分不同说话人 */
const SPEAKER_TONE_COUNT = 8;
/** 头像可读区间：低于 0.3 近黑、高于 0.58 白字发灰，都不取 */
const SPEAKER_L_MIN = 0.3;
const SPEAKER_L_MAX = 0.58;
/** 分档半幅：最深/最浅档相对主题色明度的偏移量 */
const SPEAKER_TONE_HALF_SPREAD = 0.06;

/** 兜底配色：仅在品牌色无法解析时使用 */
const SPEAKER_FALLBACK = ['#2dd4bf', '#06b6d4', '#8b5cf6', '#f59e0b', '#ef4444', '#10b981', '#ec4899', '#3b82f6'];

/** #rrggbb → HSL（h 为 0-360 度，s/l 为 0-1）；解析失败返回 null */
function hexToHsl(hex: string): { h: number; s: number; l: number } | null {
  const m = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim());
  if (!m) return null;
  const raw = m[1].length === 3 ? m[1].split('').map((c) => c + c).join('') : m[1];
  const n = parseInt(raw, 16);
  const r = ((n >> 16) & 255) / 255;
  const g = ((n >> 8) & 255) / 255;
  const b = (n & 255) / 255;
  const max = Math.max(r, g, b);
  const min = Math.min(r, g, b);
  const l = (max + min) / 2;
  const d = max - min;
  if (d === 0) return { h: 0, s: 0, l };
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
  const h =
    max === r ? ((g - b) / d + (g < b ? 6 : 0)) * 60 : max === g ? ((b - r) / d + 2) * 60 : ((r - g) / d + 4) * 60;
  return { h, s, l };
}

/** HSL → #rrggbb */
function hslToHex(h: number, s: number, l: number): string {
  const hue = (((h % 360) + 360) % 360) / 30;
  const a = s * Math.min(l, 1 - l);
  const k = (n: number) => (n + hue) % 12;
  const f = (n: number) => l - a * Math.max(-1, Math.min(k(n) - 3, 9 - k(n), 1));
  const to = (v: number) => Math.round(Math.max(0, Math.min(1, v)) * 255).toString(16).padStart(2, '0');
  return `#${to(f(0))}${to(f(8))}${to(f(4))}`;
}

/**
 * 说话人头像色板：色相与饱和度完全取自当前主题色（品牌色），只用明度深浅分档，
 * 因此 6 套色系下头像就是该色系自己的颜色（靛青是蓝、青瓷是绿、石墨是灰），
 * 同时不同说话人仍可通过深浅区分；色系或明暗切换时自动重算。
 */
const speakerPalette = computed<string[]>(() => {
  const meta = presetMeta(themeStore.preset);
  const base = hexToHsl(themeStore.isDark ? meta.primaryDark : meta.primaryLight);
  if (!base) return SPEAKER_FALLBACK;
  // 以主题色自身明度为中心，在可读区间内上下分档；半幅按剩余空间收敛，
  // 避免深浅档被裁到边界后出现重复色（两个说话人撞色）。
  const center = Math.min(
    Math.max(base.l, SPEAKER_L_MIN + SPEAKER_TONE_HALF_SPREAD),
    SPEAKER_L_MAX - SPEAKER_TONE_HALF_SPREAD,
  );
  const span = Math.min(SPEAKER_TONE_HALF_SPREAD, center - SPEAKER_L_MIN, SPEAKER_L_MAX - center);
  const half = (SPEAKER_TONE_COUNT - 1) / 2;
  return Array.from({ length: SPEAKER_TONE_COUNT }, (_, i) =>
    hslToHex(base.h, base.s, center + ((i - half) / half) * span),
  );
});

/** 由说话人姓名稳定映射到色板下标：同名同色，换页/重转写后依然一致 */
function speakerColorIndex(name: string): number {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return h % SPEAKER_TONE_COUNT;
}

/**
 * 将相对音频开头的毫秒偏移格式化为说话时间（MM:SS，超 1 小时显示 HH:MM:SS）。
 * 与音频播放器时间轴、段落跳转、逐字高亮、导出文件使用同一坐标系，
 * 不依赖会议的 start_time（对离线转写/重新转写，start_time 与音频录制时间无关）。
 */
function formatOffset(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const totalSec = Math.floor(ms / 1000);
  const h = Math.floor(totalSec / 3600);
  const m = Math.floor((totalSec % 3600) / 60);
  const s = totalSec % 60;
  const pad = (n: number) => String(n).padStart(2, '0');
  return h > 0 ? `${pad(h)}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`;
}

/**
 * 解析后端 word_timestamps（新版为 JSON 数组，旧版为 JSON 字符串），
 * 并归一化为「每个字/词都有长度 > 0、且互不重叠」的时间区间。
 *
 * 归一化是逐字高亮可靠性的前提：
 *  - 零宽区间（start === end）永远不包含播放头，既无法高亮为「正在播放」，
 *    点击又只能停在区间起点，历史上表现为「点不中 / 高亮串字」；
 *  - 互相重叠的区间会让多个字同时高亮。
 * 后端的 WordsFromCharTimesIntervals 已保证区间首尾相接，这里主要兼容
 * 历史数据（早期实时转写把句子起点误当作词区间下界，产生首字零宽）与异常数据。
 */
function parseWordTimestamps(t: MeetingTranscriptDTO): WordSpan[] {
  if (!t || !t.word_timestamps) return [];
  let raw: unknown;
  if (typeof t.word_timestamps === 'string') {
    try {
      raw = JSON.parse(t.word_timestamps);
    } catch {
      return [];
    }
  } else {
    raw = t.word_timestamps;
  }
  if (!Array.isArray(raw)) return [];
  const segEnd = Number(t.end_ms);
  const spans: WordSpan[] = [];
  for (const w of raw) {
    if (!w || typeof w.word !== 'string') continue;
    const start = Number(w.start_ms);
    const end = Number(w.end_ms);
    if (!Number.isFinite(start)) continue;
    // 后端只给“起始时刻”（end_ms === start_ms）或结束时刻非法时先记为起点，
    // 下面统一推算一个有效的结束时刻。
    spans.push({ word: w.word, startMs: start, endMs: Number.isFinite(end) && end > start ? end : start });
  }
  normalizeWordSpans(spans, segEnd);
  return spans;
}

/**
 * 就地归一化字/词时间区间，保证：
 *  1. 每个区间长度 > 0（否则无法高亮）；
 *  2. 区间之间互不重叠、单调不减；
 *  3. 不把字与字之间的停顿/静音整段算到前一个字上——补足时长时以
 *     「按词长估计的最长时长」为上界，且不得越过下一个字的起点。
 *
 * 对零宽区间先尝试向后补足（受下一个字起点与词长上界约束）；若身后没有
 * 空间（下一个字与本字同起点，如早期数据里首字被压成 [t, t] 而第二字也从 t 开始），
 * 则改为向前回退一个常规发音时长，把区间“让”给同一个起点上的后一个字。
 */
function normalizeWordSpans(spans: WordSpan[], segEnd: number): void {
  if (!spans.length) return;
  const MIN_SPAN_MS = 1;
  // nextStarts[i] = 第 i 个字之后「起点更大」的第一个字的起点，-1 表示没有。
  const nextStarts = new Array<number>(spans.length).fill(-1);
  let candidate = -1;
  for (let i = spans.length - 1; i >= 0; i--) {
    nextStarts[i] = candidate;
    candidate = spans[i].startMs;
  }

  let prevEnd = -1;
  for (let i = 0; i < spans.length; i++) {
    const cur = spans[i];

    if (cur.endMs <= cur.startMs) {
      const nextStart = nextStarts[i];
      if (nextStart > cur.startMs) {
        // 身后有空间：向后补足，受「按词长估计的最长时长」与「下一个字起点」双重约束。
        let end = cur.startMs + estimatedWordDurationMs(cur.word);
        if (nextStart < end) end = nextStart;
        if (i === spans.length - 1 && Number.isFinite(segEnd) && segEnd > cur.startMs && segEnd < end) {
          end = segEnd;
        }
        cur.endMs = end;
      } else {
        // 身后没有空间（下一个字与本字同起点，早期实时转写的首字正是这种数据）：
        // 改为向前回退一个常规发音时长，把区间让给同一个起点上的后一个字。
        const originalStart = cur.startMs;
        const floor = prevEnd >= 0 ? prevEnd : 0;
        cur.startMs = Math.max(floor, originalStart - nominalWordDurationMs(cur.word));
        cur.endMs = Math.max(originalStart, cur.startMs + MIN_SPAN_MS);
      }
    }

    // 与前一个字重叠（历史数据）：对齐到前一个字结束，保证不会同时高亮两个字。
    if (prevEnd >= 0 && cur.startMs < prevEnd) {
      cur.startMs = prevEnd;
    }
    if (cur.endMs < cur.startMs) cur.endMs = cur.startMs;
    if (cur.endMs <= cur.startMs) cur.endMs = cur.startMs + MIN_SPAN_MS;
    prevEnd = cur.endMs;
  }
}

/** 单个字/词按词长估计的常规发音时长（毫秒），用于零宽区间向前回退 */
function nominalWordDurationMs(word: string): number {
  let total = 0;
  let n = 0;
  for (const ch of word) {
    n++;
    total += isCJKChar(ch) ? 300 : 120;
  }
  return n === 0 || total <= 0 ? 300 : total;
}

/** 是否为汉字（与后端 unicode.Is(unicode.Han) 语义一致，含扩展区） */
function isCJKChar(ch: string): boolean {
  return /[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/.test(ch);
}

/** 单个字/词按词长估计的最长合理时长（毫秒）：
 *  汉字按 500ms/字、其余字符按 200ms/字符累加；空词兜底 500ms。
 *  与后端 transcript.estimatedWordDurationMs 的口径保持一致。 */
function estimatedWordDurationMs(word: string): number {
  let total = 0;
  let n = 0;
  for (const ch of word) {
    n++;
    total += isCJKChar(ch) ? 500 : 200;
  }
  return n === 0 || total <= 0 ? 500 : total;
}

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
const waveRef = ref<HTMLElement | null>(null);
const wave = shallowRef<WaveSurfer | null>(null);
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
const playing = ref(false);
const ready = ref(false);
const currentTime = ref(0);
const duration = ref(0);
/** 当前播放会话的起点（毫秒）：点击文字/段落跳转时设定，高亮"已播放"从这一点开始 */
const playAnchorMs = ref(0);
const speeds = [0.75, 1, 1.25, 1.5, 2];
const speedIndex = ref(1);
const speed = computed(() => speeds[speedIndex.value]);

// ---- 音量：本地持久化，重进详情页沿用上次设置 ----
const VOLUME_STORAGE_KEY = 'koi:meeting-audio-volume';
const volume = ref(readStoredVolume());
const muted = ref(false);

function readStoredVolume(): number {
  try {
    const raw = localStorage.getItem(VOLUME_STORAGE_KEY);
    if (raw === null) return 1;
    const n = Number(raw);
    return Number.isFinite(n) && n >= 0 && n <= 1 ? n : 1;
  } catch {
    return 1;
  }
}

function applyVolume() {
  const ws = wave.value;
  if (!ws) return;
  ws.setVolume(volume.value);
  ws.setMuted(muted.value);
}

function toggleMute() {
  if (!wave.value) return;
  muted.value = !muted.value;
  applyVolume();
}

function onVolumeChange(v: number) {
  volume.value = v;
  // 拖到 0 视为静音；从 0 往上拖自动解除静音
  muted.value = v === 0;
  try {
    localStorage.setItem(VOLUME_STORAGE_KEY, String(v));
  } catch {
    // 隐私模式等场景写入失败可忽略，不影响本次播放
  }
  applyVolume();
}

/** 读取全局设计令牌的当前取值，供 canvas 波形取色（CSS 变量在 canvas 中不可用） */
function cssVar(name: string, fallback: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}

/** 把十六进制颜色转成带透明度的 rgba；非 hex（rgb()/命名色）原样返回，避免生成非法颜色 */
function withAlpha(color: string, alpha: number): string {
  const hex = color.trim().replace('#', '');
  const full = hex.length === 3 ? hex.split('').map((c) => c + c).join('') : hex;
  if (!/^[0-9a-f]{6}$/i.test(full)) return color;
  const n = parseInt(full, 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

/** 波形配色跟随当前色系与明暗（不再硬编码靛蓝），切换主题时由 setOptions 实时刷新 */
function wavePalette() {
  const brand = cssVar('--color-brand', '#2f54eb');
  return {
    waveColor: withAlpha(brand, 0.3),
    progressColor: brand,
    cursorColor: cssVar('--color-brand-active', brand),
  };
}

function fmt(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) sec = 0;
  const m = Math.floor(sec / 60);
  const s = Math.floor(sec % 60);
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

/** 毫秒级时间格式化（MM:SS.mmm），用于逐字高亮 tooltip */
function fmtMs(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const totalSec = ms / 1000;
  const m = Math.floor(totalSec / 60);
  const s = Math.floor(totalSec % 60);
  const msPart = Math.round(ms % 1000);
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}.${String(msPart).padStart(3, '0')}`;
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
  playAnchorMs.value = 0;
}

function initWave() {
  destroyWave();
  const el = waveRef.value;
  if (!el || !audioSrc.value) return;
  const ws = WaveSurfer.create({
    container: el,
    height: 44,
    ...wavePalette(),
    cursorWidth: 2,
    barWidth: 2,
    barGap: 2,
    barRadius: 2,
    url: audioSrc.value,
  });
  // 此处 wave.value 尚未赋值（在函数末尾才绑定），音量直接作用在实例上
  ws.setVolume(volume.value);
  ws.setMuted(muted.value);
  ws.on('ready', () => {
    ready.value = true;
    duration.value = ws.getDuration();
    ws.setPlaybackRate(speed.value, false);
    // 音频元素就绪后再兜底一次（换源/重建后音量回到默认值的场景）
    applyVolume();
  });
  ws.on('timeupdate', (t: number) => {
    currentTime.value = t;
  });
  // 用户点击/拖动波形 seek：以新位置作为本轮“已播放”高亮的起点，
  // 避免拖动后整段旧位置的字仍停留在 played 高亮状态。
  ws.on('interaction', () => {
    playAnchorMs.value = ws.getCurrentTime() * 1000;
  });
  ws.on('play', () => (playing.value = true));
  ws.on('pause', () => (playing.value = false));
  ws.on('finish', () => {
    playing.value = false;
    currentTime.value = 0;
    playAnchorMs.value = 0;
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
  playAnchorMs.value = item.startMs;
  ws.setTime(item.startMs / 1000);
  if (!ws.isPlaying()) ws.play().catch(() => message.warning('音频加载失败，无法播放'));
}
/** 点击某词/字，从该词对应的精确时间点开始播放；高亮从此字开始 */
function seekToSpan(item: TranscriptItem, span: WordSpan) {
  if (!audioSrc.value) {
    message.info('该会议暂无音频');
    return;
  }
  const ws = wave.value;
  if (!ws) return;
  playAnchorMs.value = span.startMs;
  ws.setTime(span.startMs / 1000);
  if (!ws.isPlaying()) ws.play().catch(() => message.warning('音频加载失败，无法播放'));
}

/** 当前播放位置（毫秒，相对音频开头），由 wavesurfer 的 timeupdate 驱动 */
const currentMs = computed(() => currentTime.value * 1000);

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
 * 点击波形只做定位（wavesurfer 内置 seek），不再切换播放/暂停：
 * 原先的交互会让「想拖到某个位置继续听」的操作顺手把播放打断。
 * 播放/暂停交给播放键与空格键，定位与播放状态互不干扰。
 */
function skip(seconds: number) {
  const ws = wave.value;
  if (!ws || !duration.value) return;
  const t = Math.min(Math.max(ws.getCurrentTime() + seconds, 0), duration.value);
  playAnchorMs.value = t * 1000;
  ws.setTime(t);
}
function onSpeedChange(v: number) {
  const i = speeds.indexOf(v);
  if (i >= 0) {
    speedIndex.value = i;
    wave.value?.setPlaybackRate(speed.value, false);
  }
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

// 明暗/色系切换后重新取色，避免波形停留在旧主题的颜色上
watch(
  () => [themeStore.mode, themeStore.preset],
  () => wave.value?.setOptions(wavePalette()),
  { flush: 'post' },
);

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
const retranscribing = ref(false);
/** 重新转写进度条状态 */
const rt = ref<{
  visible: boolean;
  status: 'pending' | 'running' | 'completed' | 'failed' | '';
  progress: number;
  step: string;
  error: string;
}>({ visible: false, status: '', progress: 0, step: '', error: '' });
let rtTimer: ReturnType<typeof setInterval> | null = null;

function stopRtPoll() {
  if (rtTimer) {
    clearInterval(rtTimer);
    rtTimer = null;
  }
}

async function handleRetranscribe() {
  if (!meeting.value || retranscribing.value || !meeting.value.audio_url) return;
  // 停止音频播放并重置页面状态（避免旧转录/播放状态残留）
  destroyWave();
  stopRtPoll();
  transcripts.value = [];
  rt.value = { visible: true, status: 'pending', progress: 0, step: '正在提交转写任务…', error: '' };
  retranscribing.value = true;
  try {
    await meetingApi.retranscribeMeeting(meetingId.value);
    pollRtProgress();
  } catch (e) {
    retranscribing.value = false;
    rt.value = { ...rt.value, status: 'failed', error: (e as { message?: string })?.message || '未知错误' };
    message.error('触发重新转写失败：' + ((e as { message?: string })?.message || '未知错误'));
  }
}

function pollRtProgress() {
  stopRtPoll();
  rtTimer = setInterval(async () => {
    try {
      const p = await meetingApi.getTranscriptionProgress(meetingId.value);
      rt.value = {
        visible: true,
        status: p.status,
        progress: p.progress || 0,
        step: p.current_step || '',
        error: p.error_message || '',
      };
      if (p.status === 'completed') {
        stopRtPoll();
        retranscribing.value = false;
        await Promise.all([loadTranscripts(), loadMeeting()]);
        message.success('重新转写完成');
        rt.value = { ...rt.value, visible: false };
      } else if (p.status === 'failed') {
        stopRtPoll();
        retranscribing.value = false;
        message.error('重新转写失败：' + (p.error_message || '未知错误'));
      }
    } catch {
      // 网络抖动时继续轮询，由完成/失败状态决定结束
    }
  }, 1500);
}

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown);
  stopRtPoll();
  destroyWave();
});

// 音频源变化（URL 或版本号变化）时重建波形（flush:'post' 确保在 DOM 渲染、waveRef 绑定后再初始化）
watch(
  audioSrc,
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
