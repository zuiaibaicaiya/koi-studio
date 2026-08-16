<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { App } from 'antdv-next';
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller';
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css';
import {
  ArrowLeftOutlined,
  TeamOutlined,
  SoundOutlined,
  TagsOutlined,
  PlayCircleOutlined,
  PauseCircleOutlined,
  StopOutlined,
  AudioOutlined,
  CalendarOutlined,
  ReloadOutlined,
} from '@antdv-next/icons';
import { socketioService } from '../../services/socketio';
import { meetingApi } from '../../services/meetingApi';
import { decodeFileToPcm16 } from '../../utils/audio';
import { useOfflineTranscribeStore } from '../../store/offlineTranscribe';
import { useSpeakerStore } from '../../store/speaker';

const { message } = App.useApp();
const router = useRouter();
const route = useRoute();

const offlineStore = useOfflineTranscribeStore();
const audioFile = computed(() => offlineStore.file);

// 实时转写上行帧：16bit 单声道 PCM，与实时转写完全一致
const PCM_CHUNK_SIZE = 512;
const TARGET_SAMPLE_RATE = 16000;
// 离线转写按略快于实时的节奏上行（后端模型消费速度快于播放，缓冲有界）
const STREAM_SPEED = 3;

interface HotWord {
  word: string;
  weight: number;
}

interface Transcript {
  id: number;
  text: string;
  speakerID: number;
  speakerName: string;
  startTime: number;
  endTime: number;
  isInterim: boolean;
  hotWords: HotWord[];
  time: string;
}

// ---- 路由参数 ----
const meetingId = ref('');
const meetingName = ref('');
const participants = ref('');
const speakerIDs = ref<string[]>([]);
const hotWords = ref<string[]>([]);

const segments = ref<Transcript[]>([]);
const transcripts = computed(() => segments.value.filter((s) => !s.isInterim));
const hotwordSet = ref<Set<string>>(new Set());
const showOnlyHotwords = ref(false);
const visibleSegments = computed(() => {
  const list = transcripts.value;
  return showOnlyHotwords.value ? list.filter((s) => s.hotWords.length > 0) : list;
});

// ---- 转写状态 ----
const running = ref(true); // 暂停 / 继续
const starting = ref(false); // 解码 / 连接中
const started = ref(false); // 用户已点击开始
const connected = ref(false);
const streaming = ref(false); // 正在上行 PCM
const finished = ref(false); // 已上行完整段
const missingFile = ref(false); // 刷新后丢失文件
const audioError = ref(''); // 文件 / 上行异常
const decodeError = ref('');

// ---- 文件流状态 ----
let pcm: Int16Array | null = null;
let totalSamples = 0;
let sentSamples = 0;
const progress = ref(0); // 0-100
const fileDurationMs = ref(0);

// 循环控制（非响应式，避免触发渲染）
let streamPaused = false;
let streamAborted = false;

const elapsed = ref(0); // 已上行音频对应的秒数

const interim = ref<{ text: string; speakerID: number; startTime: number; endTime: number }>({
  text: '',
  speakerID: 0,
  startTime: 0,
  endTime: 0,
});
const showInterim = computed(() => !!interim.value.text && !audioError.value);
const interimSpeakerName = computed(() => resolveSpeaker(interim.value.speakerID));

// 抽屉
const drawerOpen = ref(false);
const drawerKey = ref<'participants' | 'speakers' | 'hotWords'>('participants');

const speakerStore = useSpeakerStore();
const speakerList = computed(() => speakerStore.list);
const selectedParticipants = computed(() =>
  participants.value
    .split(/[,，]/)
    .map((s) => s.trim())
    .filter(Boolean),
);
const filteredParticipants = computed(() => selectedParticipants.value);
const filteredSpeakers = computed(() =>
  speakerList.value
    .filter((s) => speakerIDs.value.map(Number).includes(s.id))
    .map((s) => ({ id: s.id, name: s.name })),
);
const hotWordGroups = computed(() =>
  hotWords.value.map((id) => ({ id: Number(id), name: `热词库 #${id}`, words: [] as HotWord[] })),
);

const drawerTitle = computed(() => {
  switch (drawerKey.value) {
    case 'speakers':
      return `已选说话人 (${filteredSpeakers.value.length})`;
    case 'hotWords':
      return `已选热词库 (${hotWords.value.length})`;
    default:
      return `参会人员 (${filteredParticipants.value.length})`;
  }
});

function openDrawer(key: 'participants' | 'speakers' | 'hotWords') {
  drawerKey.value = key;
  drawerOpen.value = true;
}

const statusTag = computed(() => {
  if (audioError.value) return { color: 'error', text: '转写异常' };
  if (starting.value) return { color: 'processing', text: '准备中' };
  if (!started.value) return { color: 'default', text: '未开始' };
  if (!connected.value) return { color: 'warning', text: '连接中' };
  if (finished.value && !streaming.value) return { color: 'success', text: '转写完成' };
  if (!running.value) return { color: 'warning', text: '已暂停' };
  if (streaming.value) return { color: 'processing', text: '转写中' };
  return { color: 'success', text: '转写中' };
});

const meetingTimeLabel = ref('');

function formatDuration(totalSec: number): string {
  const t = Math.max(0, Math.floor(totalSec));
  const h = Math.floor(t / 3600);
  const m = Math.floor((t % 3600) / 60);
  const s = t % 60;
  const mm = String(m).padStart(2, '0');
  const ss = String(s).padStart(2, '0');
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`;
}
const elapsedText = computed(() => formatDuration(elapsed.value));
const totalText = computed(() => formatDuration(fileDurationMs.value / 1000));
const progressText = computed(() => `${elapsedText.value} / ${totalText.value}`);

const transcriptPanelRef = ref<HTMLElement | null>(null);
const autoScroll = ref(true);

function getScrollerEl(): HTMLElement | null {
  return transcriptPanelRef.value?.querySelector('.transcript-scroller') as HTMLElement | null;
}
function scrollToBottom() {
  nextTick(() => {
    const target = getScrollerEl();
    if (target) target.scrollTop = target.scrollHeight;
  });
}
function onTranscriptScroll() {
  const el = getScrollerEl();
  if (!el) return;
  const distance = el.scrollHeight - el.scrollTop - el.clientHeight;
  autoScroll.value = distance < 60;
}
function onWindowFocus() {
  if (socketioService.isConnected() && !connected.value) {
    connected.value = true;
    setupSocket();
  }
}
function onVisibilityChange() {
  if (document.visibilityState === 'visible') onWindowFocus();
}

// ---- 说话人解析 ----
function resolveSpeaker(speakerID: number): string {
  if (!speakerID) return '说话人';
  const sp = speakerList.value.find((s) => s.id === speakerID);
  if (sp) return sp.name;
  const idx = speakerIDs.value.map(Number).indexOf(speakerID);
  return idx >= 0 ? `说话人 ${idx + 1}` : `说话人 ${speakerID}`;
}

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
function highlight(text: string): string {
  let html = escapeHtml(text);
  for (const w of hotwordSet.value) {
    const safe = w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    html = html.replace(new RegExp(safe, 'g'), (m) => `<span class="hl">${m}</span>`);
  }
  return html;
}

function clearInterim() {
  interim.value = { text: '', speakerID: 0, startTime: 0, endTime: 0 };
}

function handleTranscript(payload: SocketTranscript) {
  if (!payload || typeof payload.text !== 'string') return;
  if (payload.isInterim) {
    interim.value = {
      text: payload.text,
      speakerID: payload.speakerID,
      startTime: payload.startTime,
      endTime: payload.endTime,
    };
    return;
  }
  clearInterim();
  const speakerName = resolveSpeaker(payload.speakerID);
  const time = formatTimestamp(payload.startTime, payload.endTime);
  const seg: Transcript = {
    id: payload.id,
    text: payload.text,
    speakerID: payload.speakerID,
    speakerName,
    startTime: payload.startTime,
    endTime: payload.endTime,
    isInterim: false,
    hotWords: payload.hotWords || [],
    time,
  };
  const last = segments.value[segments.value.length - 1];
  if (last && last.isInterim) segments.value = segments.value.slice(0, -1);
  segments.value = [...segments.value, seg];
  for (const h of seg.hotWords) hotwordSet.value.add(h.word);
  if (autoScroll.value) scrollToBottom();
}

interface SocketTranscript {
  id: number;
  text: string;
  speakerID: number;
  startTime: number;
  endTime: number;
  isInterim: boolean;
  hotWords?: HotWord[];
}

function formatTimestamp(start: number, end: number): string {
  const fmt = (v: number) => {
    const d = new Date(v * 1000);
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    const ss = String(d.getSeconds()).padStart(2, '0');
    return `${hh}:${mm}:${ss}`;
  };
  return `${fmt(start)} - ${fmt(end)}`;
}

function handleConnect() {
  connected.value = true;
  audioError.value = '';
  socketioService.emit('join-meeting', {
    meetingID: meetingId.value,
    name: meetingName.value,
    participants: participants.value,
    speakers: speakerIDs.value.map(Number),
    hotwords: hotWords.value.map(Number),
  });
}
function handleDisconnect() {
  connected.value = false;
}
function handleSocketError(err: unknown) {
  console.error('转写 socket 异常：', err);
  audioError.value = '转写连接异常，请稍后重试或刷新页面';
}

function setupSocket() {
  socketioService.on('transcript', handleTranscript);
  socketioService.on('connect', handleConnect);
  socketioService.on('disconnect', handleDisconnect);
  socketioService.on('connect_error', handleSocketError);
  socketioService.on('error', handleSocketError);
  if (socketioService.isConnected()) {
    handleConnect();
  } else {
    socketioService.connect();
  }
}

function applyMeetingData(data: {
  name?: string;
  participants?: string;
  speaker_ids?: number[];
  hot_word_library_ids?: number[];
}) {
  if (data.name) meetingName.value = data.name;
  if (data.participants) participants.value = data.participants;
  if (Array.isArray(data.speaker_ids)) speakerIDs.value = data.speaker_ids.map(String);
  if (Array.isArray(data.hot_word_library_ids))
    hotWords.value = data.hot_word_library_ids.map(String);
}

async function loadMeetingDetail() {
  if (!meetingId.value) return;
  try {
    const data = await meetingApi.getMeeting(meetingId.value);
    applyMeetingData(data);
  } catch (err) {
    message.warning((err as Error)?.message || '会议信息加载失败');
  }
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function waitForConnect(timeoutMs = 8000): Promise<void> {
  return new Promise((resolve, reject) => {
    if (socketioService.isConnected()) return resolve();
    const start = Date.now();
    const tick = () => {
      if (socketioService.isConnected()) return resolve();
      if (Date.now() - start > timeoutMs) return reject(new Error('转写服务连接超时'));
      setTimeout(tick, 100);
    };
    tick();
  });
}

async function streamPcm() {
  if (!pcm) return;
  streamAborted = false;
  streamPaused = false;
  streaming.value = true;
  while (sentSamples < totalSamples && !streamAborted) {
    if (!running.value) {
      // 用户暂停
      streamPaused = true;
      await sleep(80);
      continue;
    }
    if (streamPaused) streamPaused = false;
    if (!socketioService.isConnected()) {
      await sleep(150);
      continue;
    }
    const end = Math.min(sentSamples + PCM_CHUNK_SIZE, totalSamples);
    const frame = pcm.slice(sentSamples, end);
    try {
      socketioService.emit('with-binary', frame.buffer, 1);
    } catch {
      audioError.value = '音频上行失败，请检查转写服务连接';
      message.error(audioError.value);
      streamAborted = true;
      break;
    }
    sentSamples = end;
    progress.value = totalSamples ? (sentSamples / totalSamples) * 100 : 100;
    elapsed.value = Math.floor(sentSamples / TARGET_SAMPLE_RATE);
    const chunkMs = (PCM_CHUNK_SIZE / TARGET_SAMPLE_RATE) * 1000; // ~32ms
    await sleep(Math.max(4, chunkMs / STREAM_SPEED));
  }
  streaming.value = false;
  if (!streamAborted && sentSamples >= totalSamples) {
    if (socketioService.isConnected()) {
      socketioService.emit('with-binary', new ArrayBuffer(0), 0);
    }
    finished.value = true;
    message.success('音频已发送完毕，正在生成最终文字稿…');
  }
}

async function startTranscription() {
  if (started.value || starting.value) return;
  if (!audioFile.value) {
    missingFile.value = true;
    message.error('未找到音频文件，请返回重新选择');
    return;
  }
  starting.value = true;
  audioError.value = '';
  decodeError.value = '';
  try {
    started.value = true;
    running.value = true;
    markMeetingOngoing();
    setupSocket();
    await waitForConnect();

    message.info('正在解码音频文件…');
    const { samples, durationMs } = await decodeFileToPcm16(audioFile.value, TARGET_SAMPLE_RATE);
    pcm = samples;
    totalSamples = samples.length;
    fileDurationMs.value = durationMs;
    void streamPcm();
  } catch (err) {
    const msg = (err as Error)?.message || '启动转写失败';
    audioError.value = msg;
    decodeError.value = msg;
    message.error(msg);
    started.value = false;
    running.value = false;
  } finally {
    starting.value = false;
  }
}

function markMeetingOngoing() {
  if (!meetingId.value) return;
  meetingApi
    .startMeeting(meetingId.value)
    .then(() => {
      if (route.query.meetingId === undefined) {
        router.replace({
          name: 'offlineTranscribe',
          query: { ...route.query, meetingId: meetingId.value },
        });
      }
    })
    .catch(() => {});
}

function togglePause() {
  if (!started.value) return;
  running.value = !running.value;
  if (!running.value) {
    clearInterim();
    message.info('已暂停转写');
  } else {
    message.info('继续转写');
  }
}

async function teardown(sendFinal = true) {
  streamAborted = true;
  streaming.value = false;
  if (sendFinal && socketioService.isConnected()) {
    try {
      socketioService.emit('with-binary', new ArrayBuffer(0), 0);
      await sleep(300);
    } catch {
      /* ignore */
    }
  }
  socketioService.disconnect();
  connected.value = false;
}

async function finalizeAndLeave(leave: () => void) {
  running.value = false;
  await teardown(true);
  const id = meetingId.value ? Number(meetingId.value) : 0;
  if (!id) {
    leave();
    return;
  }
  const hasTranscript = segments.value.length > 0;
  try {
    await meetingApi.finishMeeting(id);
    if (!hasTranscript) {
      await meetingApi.deleteMeeting(id);
    }
    leave();
  } catch (err) {
    message.warning((err as Error)?.message || '结束会议失败，请重试');
  }
}

function stopMeeting() {
  if (!started.value) {
    router.push({ name: 'home' });
    return;
  }
  finalizeAndLeave(() => router.push({ name: 'home' }));
}

function backToHome() {
  if (started.value && transcripts.value.length > 0) {
    finalizeAndLeave(() => router.push({ name: 'home' }));
  } else {
    teardown(false);
    router.push({ name: 'home' });
  }
}

function goSelectFile() {
  if (started.value && transcripts.value.length > 0) {
    finalizeAndLeave(() => router.push({ name: 'offlineCreate' }));
  } else {
    teardown(false);
    router.push({ name: 'offlineCreate' });
  }
}

onMounted(() => {
  missingFile.value = !audioFile.value;
  meetingId.value = String(route.query.meetingId || '');
  meetingName.value = String(route.query.name || '离线转写会议');
  participants.value = String(route.query.participants || '');
  speakerIDs.value = String(route.query.speakers || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  hotWords.value = String(route.query.hotWords || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);

  if (route.query.startTime && route.query.endTime) {
    meetingTimeLabel.value = `${route.query.startTime} ~ ${route.query.endTime}`;
  } else {
    const now = new Date();
    const end = new Date(now.getTime() + 60 * 60 * 1000);
    const fmt = (d: Date) => d.toLocaleString('zh-CN', { hour12: false });
    meetingTimeLabel.value = `${fmt(now)} ~ ${fmt(end)}`;
  }

  speakerStore.load().catch(() => {});
  loadMeetingDetail();
  window.addEventListener('focus', onWindowFocus);
  document.addEventListener('visibilitychange', onVisibilityChange);
});

onBeforeUnmount(() => {
  window.removeEventListener('focus', onWindowFocus);
  document.removeEventListener('visibilitychange', onVisibilityChange);
  teardown(false);
});

watch(
  () => route.query,
  () => {
    if (route.query.meetingId) {
      meetingId.value = String(route.query.meetingId);
      meetingName.value = String(route.query.name || meetingName.value);
    }
  },
);
</script>

<template>
  <div class="offline-transcribe">
    <!-- 顶部条形栏 -->
    <div class="top-bar">
      <div class="meeting-info">
        <a-button type="text" class="back-btn" @click="backToHome">
          <template #icon><ArrowLeftOutlined /></template>
        </a-button>
        <div class="info-main">
          <h2>{{ meetingName }}</h2>
          <div class="info-meta">
            <span class="meta-item">
              <AudioOutlined /> 离线转写
            </span>
            <span class="meta-item" :title="audioFile ? audioFile.name : ''">
              <AudioOutlined />
              <span class="file-name">{{ audioFile ? audioFile.name : '未选择文件' }}</span>
            </span>
            <span v-if="meetingTimeLabel" class="meta-item">
              <CalendarOutlined /> {{ meetingTimeLabel }}
            </span>
            <span v-if="started" class="meta-item progress-meta">
              <span class="progress-track">
                <span class="progress-fill" :style="{ width: progress + '%' }"></span>
              </span>
              <span class="progress-text">{{ progressText }}</span>
            </span>
          </div>
        </div>
      </div>
      <div class="top-actions">
        <a-button @click="openDrawer('participants')">
          <template #icon><TeamOutlined /></template>
          参会人员
        </a-button>
        <a-button @click="openDrawer('speakers')">
          <template #icon><SoundOutlined /></template>
          说话人
        </a-button>
        <a-button @click="openDrawer('hotWords')">
          <template #icon><TagsOutlined /></template>
          热词库
        </a-button>
        <a-button @click="goSelectFile">
          <template #icon><ReloadOutlined /></template>
          重新选择
        </a-button>
        <a-button v-if="!started || starting" type="primary" :loading="starting" @click="startTranscription">
          <template #icon><PlayCircleOutlined /></template>
          {{ starting ? '正在准备…' : '开始转写' }}
        </a-button>
        <template v-if="started && !starting">
          <a-button :disabled="audioError" @click="togglePause">
            <template #icon>
              <component :is="running ? PauseCircleOutlined : PlayCircleOutlined" />
            </template>
            {{ running ? '暂停' : '继续' }}
          </a-button>
          <a-button danger @click="stopMeeting"><StopOutlined />结束</a-button>
        </template>
      </div>
    </div>

    <a-card class="transcript-card" variant="borderless">
      <template #title>
        <span class="live-title">
          <span class="live-dot" :class="{ paused: !started || !running || !streaming }"></span>
          离线转写
          <a-tag :color="statusTag.color">{{ statusTag.text }}</a-tag>
          <a-checkbox v-model:checked="showOnlyHotwords" class="hotword-filter">
            仅看热词
          </a-checkbox>
        </span>
      </template>

      <a-alert
        v-if="missingFile"
        class="capture-alert"
        type="warning"
        show-icon
        message="未找到音频文件，可能页面已被刷新"
        description="请重新选择音频文件后开始离线转写。"
      >
        <template #action>
          <a-button size="small" type="primary" @click="goSelectFile">返回选择文件</a-button>
        </template>
      </a-alert>

      <a-alert
        v-else-if="audioError"
        class="capture-alert"
        type="error"
        show-icon
        :message="audioError"
      />

      <a-alert
        v-else-if="decodeError"
        class="capture-alert"
        type="error"
        show-icon
        :message="'音频解码失败：' + decodeError"
        description="请确认文件为浏览器可解码的音频格式（wav / mp3 / m4a 等），或重新导出后上传。"
      />

      <div ref="transcriptPanelRef" class="transcript-list">
        <div v-if="visibleSegments.length === 0 && !showInterim" class="transcript-empty">
          <AudioOutlined />
          <p v-if="!started">点击「开始转写」后，上传的音频文件将离线转写为文字稿</p>
          <p v-else-if="!running">转写已暂停，点击「继续」后将继续上行音频</p>
          <p v-else>正在解码并转写音频… 文字稿将实时显示在这里</p>
        </div>
        <DynamicScroller
          v-if="visibleSegments.length > 0"
          class="transcript-scroller"
          :items="visibleSegments"
          :min-item-size="64"
          key-field="id"
          @scroll="onTranscriptScroll"
        >
          <template #default="{ item, index, active }">
            <DynamicScrollerItem
              :item="item"
              :active="active"
              :size-dependencies="[item.text, item.speakerName]"
              :data-index="index"
            >
              <div class="transcript-item">
                <div class="seg-head">
                  <a-avatar :size="24" :style="{ backgroundColor: 'var(--color-success)' }">
                    {{ item.speakerName.charAt(0) }}
                  </a-avatar>
                  <span class="seg-speaker">{{ item.speakerName }}</span>
                  <span class="seg-time">{{ item.time }}</span>
                </div>
                <div class="seg-text" v-html="highlight(item.text)"></div>
              </div>
            </DynamicScrollerItem>
          </template>
        </DynamicScroller>

        <!-- 后端下发的中间结果：定稿前实时刷新 -->
        <div v-if="showInterim" class="transcript-item interim-item">
          <div class="seg-head">
            <a-avatar :size="24" :style="{ backgroundColor: 'var(--color-warning)' }">
              {{ interimSpeakerName.charAt(0) }}
            </a-avatar>
            <span class="seg-speaker interim-speaker">{{ interimSpeakerName }}</span>
            <span class="seg-time">识别中…</span>
          </div>
          <div class="seg-text interim-text" v-html="highlight(interim.text)"></div>
        </div>
      </div>
    </a-card>

    <a-drawer
      v-model:open="drawerOpen"
      :title="drawerTitle"
      :size="460"
      placement="right"
    >
      <div class="drawer-body">
        <!-- 参会人员 -->
        <template v-if="drawerKey === 'participants'">
          <div class="detail-list">
            <div v-for="name in filteredParticipants" :key="name" class="detail-item">
              <div class="detail-main">
                <div class="detail-title">
                  <span>{{ name }}</span>
                </div>
              </div>
            </div>
            <a-empty v-if="filteredParticipants.length === 0" description="暂无匹配的参会人员" />
          </div>
        </template>

        <!-- 说话人 -->
        <template v-else-if="drawerKey === 'speakers'">
          <div class="detail-list">
            <div v-for="s in filteredSpeakers" :key="s.id" class="detail-item">
              <div class="detail-main">
                <div class="detail-title">
                  <span>{{ s.name }}</span>
                </div>
              </div>
            </div>
            <a-empty v-if="filteredSpeakers.length === 0" description="暂无匹配的说话人" />
          </div>
        </template>

        <!-- 热词库 -->
        <template v-else>
          <div class="detail-list">
            <div v-for="g in hotWordGroups" :key="g.id" class="word-group">
              <div class="group-title">{{ g.name }}</div>
              <div v-for="w in g.words" :key="w.word" class="detail-item">
                <div class="detail-main">
                  <div class="detail-title">
                    <span>{{ w.word }}</span>
                    <a-tag color="gold">权重 {{ w.weight }}</a-tag>
                  </div>
                </div>
              </div>
              <a-empty v-if="g.words.length === 0" description="该热词库暂无热词" />
            </div>
            <a-empty v-if="hotWordGroups.length === 0" description="暂无启用的热词库" />
          </div>
        </template>
      </div>
    </a-drawer>
  </div>
</template>

<style scoped>
.offline-transcribe {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-height: 100vh;
  box-sizing: border-box;
  padding: 20px 24px 32px;
  max-width: 1200px;
  margin: 0 auto;
  color: var(--color-text);
  background: radial-gradient(
      1200px 600px at 80% -10%,
      var(--color-brand-soft),
      transparent 60%
    ),
    var(--color-bg);
  --radius-lg: 18px;
  --radius-md: 12px;
  --radius-sm: 8px;
  --shadow-card: var(--shadow-sm);
  --transition-base: 0.25s cubic-bezier(0.4, 0, 0.2, 1);
}
.top-bar {
  position: sticky;
  top: 0;
  z-index: 10;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 14px 18px;
  box-shadow: var(--shadow-card);
}
.meeting-info {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  flex-shrink: 1;
}
.back-btn {
  color: var(--color-text-secondary);
}
.back-btn:hover {
  color: var(--color-brand-hover) !important;
}
.info-main h2 {
  margin: 0;
  color: var(--color-text);
  font-size: 18px;
  font-weight: 600;
}
.info-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  margin-top: 4px;
  color: var(--color-text-muted);
  font-size: 12px;
}
.meta-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
}
.file-name {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.progress-meta {
  min-width: 180px;
}
.progress-track {
  display: inline-block;
  width: 120px;
  height: 6px;
  border-radius: 3px;
  background: var(--color-surface-2);
  overflow: hidden;
}
.progress-fill {
  display: block;
  height: 100%;
  border-radius: 3px;
  background: linear-gradient(90deg, #52c41a, #73d13d);
  transition: width 0.2s linear;
}
.progress-text {
  font-variant-numeric: tabular-nums;
  font-size: 12px;
  color: var(--color-text-secondary);
}
.top-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
}
.capture-alert {
  margin-bottom: 12px;
}
.transcript-card :deep(.ant-card-head-title) {
  color: var(--color-text);
}
.detail-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 0;
}

/* 抽屉内容 */
.drawer-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.detail-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.detail-item {
  display: flex;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--color-border-secondary);
}
.detail-item:last-child {
  border-bottom: none;
}
.detail-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-items: flex-start;
}
.detail-title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  color: var(--color-text);
  font-size: 14px;
  font-weight: 600;
}
.word-group + .word-group {
  margin-top: 8px;
}
.group-title {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-top: 8px;
  color: var(--color-text-secondary);
  font-size: 12px;
  font-weight: 600;
}
.transcript-card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  height: 100%;
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-card);
}
.live-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.hotword-filter {
  margin-left: 8px;
  font-size: 12px;
  font-weight: 400;
}
.live-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--color-success);
  box-shadow: 0 0 0 0 rgba(82, 196, 26, 0.6);
  animation: pulse 1.4s infinite;
}
.live-dot.paused {
  background: var(--color-warning);
  animation: none;
}
@keyframes pulse {
  0% { box-shadow: 0 0 0 0 rgba(82, 196, 26, 0.6); }
  70% { box-shadow: 0 0 0 8px rgba(82, 196, 26, 0); }
  100% { box-shadow: 0 0 0 0 rgba(82, 196, 26, 0); }
}
.transcript-list {
  min-height: 200px;
}
.transcript-scroller {
  height: 60vh;
  overflow-y: auto;
  padding-right: 8px;
  scroll-behavior: smooth;
}
.transcript-scroller::-webkit-scrollbar {
  width: 6px;
}
.transcript-scroller::-webkit-scrollbar-thumb {
  background: var(--color-border-strong);
  border-radius: 3px;
}
.transcript-scroller::-webkit-scrollbar-thumb:hover {
  background: var(--color-text-muted);
}
.transcript-empty {
  text-align: center;
  color: var(--color-text-muted);
  padding: 48px 0;
}
.transcript-empty :deep(> .anticon) {
  font-size: 36px;
  display: block;
  margin-bottom: 12px;
}
.transcript-item {
  padding: 14px 16px;
  background: var(--color-surface-2);
  border: 1px solid var(--color-border);
  border-radius: 12px;
  margin-bottom: 12px;
  animation: fadeInUp 0.35s cubic-bezier(0.16, 1, 0.3, 1);
}
@keyframes fadeInUp {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
.seg-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  position: sticky;
  top: 0;
}
.seg-speaker {
  font-weight: 600;
  color: var(--color-text);
  font-size: 14px;
}
.seg-time {
  color: var(--color-text-muted);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.seg-text {
  color: var(--color-text);
  line-height: 1.75;
  font-size: 15px;
  word-break: break-word;
}
.seg-text :deep(.hl) {
  background: linear-gradient(180deg, transparent 60%, var(--color-brand-soft) 60%);
  color: var(--color-brand-hover);
  border-radius: 3px;
  padding: 0 2px;
  font-weight: 600;
}
.interim-item {
  border-style: dashed;
  background: var(--color-surface-2);
}
.interim-speaker {
  color: var(--color-warning);
}
.interim-text {
  color: var(--color-text-secondary);
}
</style>
